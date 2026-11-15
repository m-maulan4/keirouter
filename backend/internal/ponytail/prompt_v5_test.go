package ponytail

import (
	"strings"
	"testing"
)

// Verifikasi fragmen v5.3 khas — ladder 6 rung, keyword shortcut:, never-cut
// hardware, setup "read first".
func TestPromptContainsV53Fragments(t *testing.T) {
	for _, l := range []Level{LevelLite, LevelFull, LevelUltra} {
		p := prompts[l]
		for _, want := range []string{
			"Read the task and the code it touches",
			"Already in this codebase",
			"`shortcut: <the limit>, <when to upgrade>`",
			"the calibration real hardware needs",
			"End your reply with one or two lines",
			"Level switches on request from the client as `ponytail lite|full|ultra`",
		} {
			if !strings.Contains(p, want) {
				t.Errorf("level %q missing v5.3 fragment %q", l, want)
			}
		}
	}
}

// Ladder rung count — 6 langkah.
func TestLadderHas6Rungs(t *testing.T) {
	for _, n := range []string{"1)", "2)", "3)", "4)", "5)", "6)"} {
		if !strings.Contains(prompts[LevelFull], n) {
			t.Errorf("ladder missing rung %s", n)
		}
	}
}

// Level lines masih distinct.
func TestLevelLinesDistinct(t *testing.T) {
	seen := map[string]Level{}
	for l, line := range levelLines {
		if prev, dup := seen[line]; dup {
			t.Errorf("level %q and %q share level line", prev, l)
		}
		seen[line] = l
	}
}

// Keputusan owner: ultra murni SKILL.md — push back hanya "question the
// request", tanpa tambahan "Deletion before addition" warisan v4.
func TestUltraLineMurniSKILL(t *testing.T) {
	ultra := prompts[LevelUltra]
	if !strings.Contains(ultra, "question the request") {
		t.Errorf("ultra missing SKILL.md push-back: %q", ultra)
	}
	if strings.Contains(ultra, "Deletion before addition") {
		t.Errorf("ultra contains legacy v4 phrase, PRD says SKILL.md-only: %q", ultra)
	}
}

// Keyword komentar berubah dari ponytail: ke shortcut: di seluruh blok.
func TestNoLegacyPonytailKeyword(t *testing.T) {
	for _, l := range []Level{LevelLite, LevelFull, LevelUltra} {
		if strings.Contains(prompts[l], "`ponytail:") {
			t.Errorf("level %q still uses legacy `ponytail:` marker", l)
		}
	}
}