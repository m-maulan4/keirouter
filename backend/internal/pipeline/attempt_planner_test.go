package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mydisha/keirouter/backend/internal/config"
	"github.com/mydisha/keirouter/backend/internal/core"
	"github.com/mydisha/keirouter/backend/internal/crypto"
	"github.com/mydisha/keirouter/backend/internal/dispatch"
	"github.com/mydisha/keirouter/backend/internal/store"
	"github.com/mydisha/keirouter/backend/internal/vault"
	"github.com/stretchr/testify/require"
)

type plannerConnectorSource struct{ conn core.Connector }

func (s plannerConnectorSource) Get(string) (core.Connector, error) { return s.conn, nil }

type plannerConnector struct{}

func (plannerConnector) ID() string            { return "openai" }
func (plannerConnector) Dialect() core.Dialect { return core.DialectOpenAI }
func (plannerConnector) Chat(context.Context, *core.ChatRequest, core.Credentials) (*core.ChatResponse, error) {
	return nil, nil
}
func (plannerConnector) Stream(context.Context, *core.ChatRequest, core.Credentials, core.StreamConfig) (<-chan core.StreamChunk, error) {
	ch := make(chan core.StreamChunk)
	close(ch)
	return ch, nil
}

func newPlannerDispatcher(t *testing.T) *dispatch.Dispatcher {
	return newPlannerDispatcherWithAccounts(t, "acc-1", "acc-2")
}

func newPlannerDispatcherWithAccounts(t *testing.T, accountIDs ...string) *dispatch.Dispatcher {
	return newPlannerDispatcherWithConnector(t, plannerConnector{}, accountIDs...)
}

func newPlannerDispatcherWithConnector(t *testing.T, conn core.Connector, accountIDs ...string) *dispatch.Dispatcher {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, config.DatabaseConfig{Driver: "sqlite", DSN: ":memory:"}, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, db.Migrate(ctx))
	require.NoError(t, db.Tenants().EnsureDefault(ctx))
	t.Cleanup(func() { _ = db.Close() })

	key, err := crypto.GenerateMasterKey()
	require.NoError(t, err)
	sealer, err := crypto.NewSealer(key)
	require.NoError(t, err)
	secrets := vault.New(sealer)

	for i, id := range accountIDs {
		now := time.Now()
		account := store.Account{
			ID: id, TenantID: store.DefaultTenantID, Provider: "openai",
			Label: id, AuthKind: store.AuthAPIKey, Priority: (i + 1) * 10,
			Metadata: "{}", CreatedAt: now, UpdatedAt: now,
		}
		require.NoError(t, secrets.Seal(&account, vault.NewSecret{APIKey: "sk-test"}))
		require.NoError(t, db.Accounts().Create(ctx, account))
	}

	d := dispatch.New(plannerConnectorSource{conn: conn}, db.Accounts(), secrets)
	d.SetRoutingSource(db.Routing())
	return d
}

// newPlannerDispatcherWithCustoms creates custom-provider accounts for the
// ignore-usage-limits loop. ignore maps account id -> IgnoreUsageLimits.
func newPlannerDispatcherWithCustoms(t *testing.T, conn core.Connector, ids []string, ignore map[string]bool) (*dispatch.Dispatcher, *store.DB) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, config.DatabaseConfig{Driver: "sqlite", DSN: ":memory:"}, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, db.Migrate(ctx))
	require.NoError(t, db.Tenants().EnsureDefault(ctx))
	t.Cleanup(func() { _ = db.Close() })

	key, err := crypto.GenerateMasterKey()
	require.NoError(t, err)
	sealer, err := crypto.NewSealer(key)
	require.NoError(t, err)
	secrets := vault.New(sealer)

	for i, id := range ids {
		now := time.Now()
		account := store.Account{
			ID: id, TenantID: store.DefaultTenantID, Provider: "custom-openai",
			Label: id, AuthKind: store.AuthAPIKey, Priority: (i + 1) * 10,
			Metadata: "{}", CreatedAt: now, UpdatedAt: now,
			IgnoreUsageLimits: ignore[id],
		}
		require.NoError(t, secrets.Seal(&account, vault.NewSecret{APIKey: "sk-test"}))
		require.NoError(t, db.Accounts().Create(ctx, account))
	}

	d := dispatch.New(plannerConnectorSource{conn: conn}, db.Accounts(), secrets)
	d.SetRoutingSource(db.Routing())
	return d, db
}

func mustAttempt(t *testing.T, planner *attemptPlanner) dispatch.Attempt {
	t.Helper()
	a, ok := planner.Current()
	require.True(t, ok)
	return a
}

// TestAttemptPlannerIgnoreUsageLimitsSingleOnRetries verifies PRD Opsi A: one
// ON account parks 10s, sleeps, then is retried.
func TestAttemptPlannerIgnoreUsageLimitsSingleOnRetries(t *testing.T) {
	ctx := context.Background()
	d, db := newPlannerDispatcherWithCustoms(t, plannerConnector{}, []string{"a1"}, map[string]bool{"a1": true})
	targets := []dispatch.Target{{Provider: "custom-openai", Model: "glm-5.2"}}
	initial, err := d.Plan(ctx, store.DefaultTenantID, targets, core.NewCapabilitySet())
	require.NoError(t, err)

	planner := newAttemptPlanner(d, store.DefaultTenantID, targets, core.NewCapabilitySet(), dispatch.PlanOptions{}, initial)
	// Real dispatcher parks a1 on the DB before the planner re-plans.
	pe := &core.ProviderError{Kind: core.ErrRateLimit, Scope: core.FailureScopeAccount}
	d.NoteFailure(ctx, "a1", pe)

	var slept []time.Duration
	planner.sleep = func(_ context.Context, d time.Duration) bool {
		slept = append(slept, d)
		// Simulate the 10s passing: expire a1's cooldown so the re-plan finds it.
		require.NoError(t, db.Accounts().SetCooldown(ctx, "a1", time.Now()))
		return true
	}
	first := mustAttempt(t, planner)
	retry, ok := planner.AfterFailure(ctx, first, pe)
	require.True(t, ok, "the ON account is retried after the cooldown")
	require.Equal(t, first.Account.ID, retry.Account.ID)
	require.Equal(t, []time.Duration{dispatch.IgnoreUsageLimitsCooldown}, slept,
		"a lone cooling ON account sleeps the fixed cooldown before retrying")
	require.True(t, planner.remaining, "the loop is still live")
}

// TestAttemptPlannerIgnoreUsageLimitsTwoOnSkipsSleepWhenFresh verifies PRD
// Opsi B: with a second ON account fresh, the failed account is skipped and
// the fresh one is tried without sleeping.
func TestAttemptPlannerIgnoreUsageLimitsTwoOnSkipsSleepWhenFresh(t *testing.T) {
	ctx := context.Background()
	d, _ := newPlannerDispatcherWithCustoms(t, plannerConnector{}, []string{"a1", "a2"}, map[string]bool{"a1": true, "a2": true})
	targets := []dispatch.Target{{Provider: "custom-openai", Model: "glm-5.2"}}
	initial, err := d.Plan(ctx, store.DefaultTenantID, targets, core.NewCapabilitySet())
	require.NoError(t, err)

	planner := newAttemptPlanner(d, store.DefaultTenantID, targets, core.NewCapabilitySet(), dispatch.PlanOptions{}, initial)
	slept := false
	planner.sleep = func(context.Context, time.Duration) bool { slept = true; t.Fatal("must not sleep"); return false }
	pe := &core.ProviderError{Kind: core.ErrRateLimit, Scope: core.FailureScopeAccount}
	d.NoteFailure(ctx, "a1", pe) // park a1

	first := mustAttempt(t, planner)
	require.Equal(t, "a1", first.Account.ID)
	second, ok := planner.AfterFailure(ctx, first, pe)
	require.True(t, ok)
	require.Equal(t, "a2", second.Account.ID, "option B: a2 is fresh and taken directly")
	require.False(t, slept)
}

// TestAttemptPlannerIgnoreUsageLimitsMixOnOff follows PRD "Campuran ON/OFF":
// a1 (ON) fails 429 -> ignored loop offers the next fresh account a2 (OFF)
// directly; a2 fails 429 -> parked NORMALLY and excluded; a1 comes back after
// its short cooldown. a2 is never offered again.
func TestAttemptPlannerIgnoreUsageLimitsMixOnOff(t *testing.T) {
	ctx := context.Background()
	d, db := newPlannerDispatcherWithCustoms(t, plannerConnector{}, []string{"a1", "a2"}, map[string]bool{"a1": true})
	targets := []dispatch.Target{{Provider: "custom-openai", Model: "glm-5.2"}}
	initial, err := d.Plan(ctx, store.DefaultTenantID, targets, core.NewCapabilitySet())
	require.NoError(t, err)

	pe := &core.ProviderError{Kind: core.ErrRateLimit, Scope: core.FailureScopeAccount}
	planner := newAttemptPlanner(d, store.DefaultTenantID, targets, core.NewCapabilitySet(), dispatch.PlanOptions{}, initial)
	// Whenever the ignore loop must wait, expire a1's cooldown so it comes
	// back instead of blocking the test on a real 10s sleep.
	planner.sleep = func(_ context.Context, _ time.Duration) bool {
		require.NoError(t, db.Accounts().SetCooldown(ctx, "a1", time.Now()))
		return true
	}

	// a1 (ON) fails 429 -> ignore loop probes: no fresh ON account yet, but
	// a2 (OFF, fresh) is offered directly (option B), no sleep.
	a1 := mustAttempt(t, planner)
	require.Equal(t, "a1", a1.Account.ID)
	d.NoteFailure(ctx, "a1", pe)
	second, ok := planner.AfterFailure(ctx, a1, pe)
	require.True(t, ok)
	require.Equal(t, "a2", second.Account.ID, "option B: the next fresh account is offered without a sleep")

	// a2 (OFF) fails 429 -> parked NORMALLY (escalating backoff, exclusion).
	// The request waits for a1's cooldown and comes back to a1.
	d.NoteFailure(ctx, "a2", pe)
	third, ok := planner.AfterFailure(ctx, second, pe)
	require.True(t, ok)
	require.Equal(t, "a1", third.Account.ID, "a1 is retried; a2 is not in the ignore loop")

	// Repeat: a1 fails again and keeps the loop alive; a2 stays parked/excluded.
	d.NoteFailure(ctx, "a1", pe)
	fourth, ok := planner.AfterFailure(ctx, third, pe)
	require.True(t, ok)
	require.Equal(t, "a1", fourth.Account.ID, "only ON accounts rotate in the ignore loop")

	// Fail both accounts exhaustively; a2 is excluded and the loop still only
	// re-offers a1.
	d.NoteFailure(ctx, "a2", pe)
	fifth, ok := planner.AfterFailure(ctx, fourth, pe)
	require.True(t, ok)
	require.Equal(t, "a1", fifth.Account.ID)
}

// TestAttemptPlannerIgnoreUsageLimitsContextCancelStops verifies ctx cancel
// ends the endless loop (PRD: sleepCtx/ctx.Done() is the only exit).
func TestAttemptPlannerIgnoreUsageLimitsContextCancelStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d, _ := newPlannerDispatcherWithCustoms(t, plannerConnector{}, []string{"a1"}, map[string]bool{"a1": true})
	targets := []dispatch.Target{{Provider: "custom-openai", Model: "glm-5.2"}}
	initial, err := d.Plan(ctx, store.DefaultTenantID, targets, core.NewCapabilitySet())
	require.NoError(t, err)

	planner := newAttemptPlanner(d, store.DefaultTenantID, targets, core.NewCapabilitySet(), dispatch.PlanOptions{}, initial)
	pe := &core.ProviderError{Kind: core.ErrRateLimit, Scope: core.FailureScopeAccount}
	d.NoteFailure(ctx, "a1", pe)

	slept := 0
	planner.sleep = func(_ context.Context, _ time.Duration) bool {
		slept++
		cancel() // the client goes away mid-sleep
		return false
	}
	first := mustAttempt(t, planner)
	_, ok := planner.AfterFailure(ctx, first, pe)
	require.False(t, ok, "cancelled context stops the ignore loop")
	require.Equal(t, 1, slept)
}

func TestAttemptPlannerModelFailureKeepsAccountAvailableForNextModel(t *testing.T) {
	ctx := context.Background()
	d := newPlannerDispatcher(t)
	targets := []dispatch.Target{
		{Provider: "openai", Model: "gpt-4o"},
		{Provider: "openai", Model: "gpt-5"},
	}
	initial, err := d.Plan(ctx, store.DefaultTenantID, targets, core.NewCapabilitySet())
	require.NoError(t, err)
	planner := newAttemptPlanner(d, store.DefaultTenantID, targets, core.NewCapabilitySet(), dispatch.PlanOptions{}, initial)

	first, ok := planner.Current()
	require.True(t, ok)
	require.Equal(t, "acc-1", first.Account.ID)
	require.Equal(t, "gpt-4o", first.Target.Model)

	second, ok := planner.AfterFailure(ctx, first, &core.ProviderError{Kind: core.ErrModelUnavailable})
	require.True(t, ok)
	require.Equal(t, "acc-2", second.Account.ID)
	require.Equal(t, "gpt-4o", second.Target.Model)

	third, ok := planner.AfterFailure(ctx, second, &core.ProviderError{Kind: core.ErrModelUnavailable})
	require.True(t, ok)
	require.Equal(t, "acc-1", third.Account.ID)
	require.Equal(t, "gpt-5", third.Target.Model)
}

func TestAttemptPlannerAccountFailureSkipsCredentialAcrossModels(t *testing.T) {
	ctx := context.Background()
	d := newPlannerDispatcher(t)
	targets := []dispatch.Target{
		{Provider: "openai", Model: "gpt-4o"},
		{Provider: "openai", Model: "gpt-5"},
	}
	initial, err := d.Plan(ctx, store.DefaultTenantID, targets, core.NewCapabilitySet())
	require.NoError(t, err)
	planner := newAttemptPlanner(d, store.DefaultTenantID, targets, core.NewCapabilitySet(), dispatch.PlanOptions{}, initial)

	first, ok := planner.Current()
	require.True(t, ok)
	second, ok := planner.AfterFailure(ctx, first, &core.ProviderError{Kind: core.ErrAuth})
	require.True(t, ok)
	require.Equal(t, "acc-2", second.Account.ID)

	_, ok = planner.AfterFailure(ctx, second, &core.ProviderError{Kind: core.ErrAuth})
	require.False(t, ok)
}

func TestAttemptPlannerRepairPrefersAnotherAccount(t *testing.T) {
	ctx := context.Background()
	d := newPlannerDispatcher(t)
	targets := []dispatch.Target{{Provider: "openai", Model: "gpt-4o"}}
	initial, err := d.Plan(ctx, store.DefaultTenantID, targets, core.NewCapabilitySet())
	require.NoError(t, err)
	planner := newAttemptPlanner(d, store.DefaultTenantID, targets, core.NewCapabilitySet(), dispatch.PlanOptions{}, initial)

	first, ok := planner.Current()
	require.True(t, ok)
	second, ok := planner.AfterRepair(ctx, first, &core.ProviderError{
		Kind: core.ErrUpstream, Scope: core.FailureScopeRequest,
	})
	require.True(t, ok)
	require.Equal(t, "acc-2", second.Account.ID)
}

func TestAttemptPlannerRepairRetriesOnlyAccountOnce(t *testing.T) {
	ctx := context.Background()
	d := newPlannerDispatcherWithAccounts(t, "acc-1")
	targets := []dispatch.Target{{Provider: "openai", Model: "gpt-4o"}}
	initial, err := d.Plan(ctx, store.DefaultTenantID, targets, core.NewCapabilitySet())
	require.NoError(t, err)
	planner := newAttemptPlanner(d, store.DefaultTenantID, targets, core.NewCapabilitySet(), dispatch.PlanOptions{}, initial)

	first, ok := planner.Current()
	require.True(t, ok)
	retry, ok := planner.AfterRepair(ctx, first, &core.ProviderError{
		Kind: core.ErrUpstream, Scope: core.FailureScopeRequest,
	})
	require.True(t, ok)
	require.Equal(t, first.Key(), retry.Key())

	_, ok = planner.AfterFailure(ctx, retry, &core.ProviderError{
		Kind: core.ErrUpstream, Scope: core.FailureScopeRequest,
	})
	require.False(t, ok)
}

type repairPipelineConnector struct {
	mu       sync.Mutex
	systems  []string
	accounts []string
}

func (*repairPipelineConnector) ID() string            { return "openai" }
func (*repairPipelineConnector) Dialect() core.Dialect { return core.DialectOpenAI }
func (c *repairPipelineConnector) Chat(_ context.Context, req *core.ChatRequest, creds core.Credentials) (*core.ChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.systems = append(c.systems, req.System)
	c.accounts = append(c.accounts, creds.AccountID)
	if len(c.systems) == 1 {
		return nil, &core.ProviderError{
			Kind:                   core.ErrUpstream,
			Scope:                  core.FailureScopeRequest,
			Message:                "incomplete response",
			RetrySystemInstruction: "complete the previous response",
			AttemptUsage: &core.Usage{
				PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6,
				Source: core.UsageSourceProvider,
			},
		}
	}
	return &core.ChatResponse{
		Model: req.Model,
		Message: core.Message{
			Role:    core.RoleAssistant,
			Content: []core.ContentPart{{Type: core.PartText, Text: "done"}},
		},
		FinishReason: core.FinishStop,
		Usage: core.Usage{
			PromptTokens: 6, CompletionTokens: 2, TotalTokens: 8,
			Source: core.UsageSourceProvider,
		},
	}, nil
}
func (*repairPipelineConnector) Stream(context.Context, *core.ChatRequest, core.Credentials, core.StreamConfig) (<-chan core.StreamChunk, error) {
	ch := make(chan core.StreamChunk)
	close(ch)
	return ch, nil
}

func TestPipelineRepairReroutesAndAccountsForBothAttempts(t *testing.T) {
	conn := &repairPipelineConnector{}
	d := newPlannerDispatcherWithConnector(t, conn, "acc-1", "acc-2")
	p := New(Deps{Dispatcher: d})
	req := &core.ChatRequest{
		Model:  "gpt-4o",
		System: "original",
		Messages: []core.Message{{
			Role: core.RoleUser, Content: []core.ContentPart{{Type: core.PartText, Text: "hello"}},
		}},
		Metadata: core.RequestMetadata{TenantID: store.DefaultTenantID},
	}

	result, err := p.Chat(context.Background(), req, Options{
		Targets: []dispatch.Target{{Provider: "openai", Model: "gpt-4o"}},
	})
	require.NoError(t, err)
	require.Equal(t, "acc-2", result.AccountID)
	require.Equal(t, 11, result.Response.Usage.PromptTokens)
	require.Equal(t, 3, result.Response.Usage.CompletionTokens)
	require.Equal(t, 14, result.Response.Usage.TotalTokens)
	require.Equal(t, "original", req.System)

	conn.mu.Lock()
	defer conn.mu.Unlock()
	require.Equal(t, []string{"acc-1", "acc-2"}, conn.accounts)
	require.Equal(t, []string{
		"original",
		"original\n\ncomplete the previous response",
	}, conn.systems)
}

func TestAttemptPlannerRetriesSameAttemptWhenChainExhausted(t *testing.T) {
	ctx := context.Background()
	d := newPlannerDispatcherWithAccounts(t, "acc-only")
	targets := []dispatch.Target{{Provider: "openai", Model: "gpt-4o"}}
	initial, err := d.PlanWith(ctx, store.DefaultTenantID, targets, core.NewCapabilitySet(), dispatch.PlanOptions{})
	require.NoError(t, err)

	planner := newAttemptPlanner(d, store.DefaultTenantID, targets, core.NewCapabilitySet(), dispatch.PlanOptions{}, initial)
	var slept []time.Duration
	planner.sleep = func(_ context.Context, d time.Duration) bool {
		slept = append(slept, d)
		return true
	}
	first, ok := planner.Current()
	require.True(t, ok)

	transient := &core.ProviderError{Kind: core.ErrUpstream, Scope: core.FailureScopeProvider, StatusCode: 502}
	retry, ok := planner.AfterFailure(ctx, first, transient)
	require.True(t, ok, "a lone transient fault on a single-account chain is retried")
	require.Equal(t, first.Account.ID, retry.Account.ID)

	retry, ok = planner.AfterFailure(ctx, retry, transient)
	require.True(t, ok)
	require.Equal(t, first.Account.ID, retry.Account.ID)

	_, ok = planner.AfterFailure(ctx, retry, transient)
	require.False(t, ok, "retries are bounded")

	require.Len(t, slept, maxSameAttemptRetries)
	require.GreaterOrEqual(t, slept[0], sameRetryInitialDelay)
	require.LessOrEqual(t, slept[0], sameRetryInitialDelay+sameRetryJitter)
	require.GreaterOrEqual(t, slept[1], 2*sameRetryInitialDelay)
}

func TestAttemptPlannerDoesNotRetrySameAttemptForNonTransientErrors(t *testing.T) {
	ctx := context.Background()
	d := newPlannerDispatcherWithAccounts(t, "acc-only")
	targets := []dispatch.Target{{Provider: "openai", Model: "gpt-4o"}}
	initial, err := d.PlanWith(ctx, store.DefaultTenantID, targets, core.NewCapabilitySet(), dispatch.PlanOptions{})
	require.NoError(t, err)

	for _, pe := range []*core.ProviderError{
		{Kind: core.ErrAuth},
		{Kind: core.ErrRateLimit},
		{Kind: core.ErrContextWindow},
		{Kind: core.ErrTimeout, Scope: core.FailureScopeRequest, Cause: context.DeadlineExceeded},
	} {
		planner := newAttemptPlanner(d, store.DefaultTenantID, targets, core.NewCapabilitySet(), dispatch.PlanOptions{}, initial)
		planner.sleep = func(context.Context, time.Duration) bool { t.Fatal("must not sleep"); return false }
		first, _ := planner.Current()
		_, ok := planner.AfterFailure(ctx, first, pe)
		require.False(t, ok, "kind %s", pe.Kind)
	}
}

func TestSameRetryDelayHonoursShortRetryAfter(t *testing.T) {
	d := sameRetryDelay(0, 3*time.Second)
	require.GreaterOrEqual(t, d, 3*time.Second)
	require.LessOrEqual(t, d, 3*time.Second+sameRetryJitter)

	d = sameRetryDelay(0, 10*time.Minute) // too long: fall back to exponential
	require.LessOrEqual(t, d, sameRetryInitialDelay+sameRetryJitter)

	d = sameRetryDelay(10, 0)
	require.LessOrEqual(t, d, sameRetryMaxDelay+sameRetryJitter)
}
