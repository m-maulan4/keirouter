package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/mydisha/keirouter/backend/internal/store"
	"github.com/mydisha/keirouter/backend/internal/vault"
)

func TestAdminUpdateAccountProxyPool(t *testing.T) {
	s, db := newBulkTestServer(t)
	s.pools = db.ProxyPools()
	now := time.Now()
	pool := store.ProxyPool{
		ProxyURL: "https://relay.example.com", IsActive: true,
		TestStatus: "unknown", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, s.pools.Create(context.Background(), pool))
	acc := store.Account{
		ID: uuid.NewString(), TenantID: adminTenant, Provider: "openai",
		Label: "account", AuthKind: store.AuthAPIKey, Priority: 100,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, s.vault.Seal(&acc, vault.NewSecret{APIKey: "secret", Metadata: map[string]string{}}))
	require.NoError(t, s.accounts.Create(context.Background(), acc))

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", acc.ID)
	req := httptest.NewRequest(http.MethodPatch, "/accounts/"+acc.ID, strings.NewReader(`{"proxy_pool_id":"`+pool.ID+`"}`))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.adminUpdateAccount(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	updated, err := s.accounts.Get(context.Background(), acc.ID)
	require.NoError(t, err)
	require.Equal(t, pool.ID, updated.ProxyPoolID)
}

func TestAdminUpdateAccountIgnoreUsageLimits(t *testing.T) {
	s, _ := newBulkTestServer(t)
	ctx := context.Background()
	now := time.Now()
	custom := store.Account{
		ID: uuid.NewString(), TenantID: adminTenant, Provider: "custom-openai-lab",
		Label: "custom", AuthKind: store.AuthAPIKey, Priority: 100,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, s.vault.Seal(&custom, vault.NewSecret{APIKey: "secret"}))
	require.NoError(t, s.accounts.Create(ctx, custom))

	patch := func(id, body string) *httptest.ResponseRecorder {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id)
		req := httptest.NewRequest(http.MethodPatch, "/accounts/"+id, strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		s.adminUpdateAccount(rec, req)
		return rec
	}

	// A custom provider account accepts the toggle.
	rec := patch(custom.ID, `{"ignore_usage_limits":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	updated, err := s.accounts.Get(ctx, custom.ID)
	require.NoError(t, err)
	require.True(t, updated.IgnoreUsageLimits)

	rec = patch(custom.ID, `{"ignore_usage_limits":false}`)
	require.Equal(t, http.StatusOK, rec.Code)
	updated, err = s.accounts.Get(ctx, custom.ID)
	require.NoError(t, err)
	require.False(t, updated.IgnoreUsageLimits)

	// A built-in provider account is hard-rejected even when the flag claims true.
	builtin := store.Account{
		ID: uuid.NewString(), TenantID: adminTenant, Provider: "openai",
		Label: "builtin", AuthKind: store.AuthAPIKey, Priority: 100,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, s.vault.Seal(&builtin, vault.NewSecret{APIKey: "secret"}))
	require.NoError(t, s.accounts.Create(ctx, builtin))
	rec = patch(builtin.ID, `{"ignore_usage_limits":true}`)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	updated, err = s.accounts.Get(ctx, builtin.ID)
	require.NoError(t, err)
	require.False(t, updated.IgnoreUsageLimits)
}
