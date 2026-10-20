package server

import (
	"context"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/config"
)

// reported is the last report as sorted "user target mode" lines.
func (c *linkCore) reported() ([]string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.modes))
	for _, m := range c.modes {
		out = append(out, m["user"]+" "+m["target"]+" "+m["mode"])
	}
	sort.Strings(out)
	return out, c.nModes
}

// waitModes waits for a report (after the `after`-th) carrying exactly exp.
func (lk *linkKitchen) waitModes(t *testing.T, what string, after int, exp ...string) int {
	t.Helper()
	sort.Strings(exp)
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, n := lk.core.reported()
		if n > after && strings.Join(got, "\n") == strings.Join(exp, "\n") {
			return n
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: reports %d (after %d)\n got  %q\n want %q", what, n, after, got, exp)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Kitchen tells Core how each person connects to Inventory, Split and Habits:
// the mode in effect (defaults included) for everybody in one call after
// registering, and again right after someone changes a mode or disconnects.
func TestModesReportedToCore(t *testing.T) {
	lk := newLinkKitchen(t, func(cfg *config.Config, lk *linkKitchen) {
		cfg.InventoryURL, cfg.InventoryToken = lk.inv.URL, "env-inv"
	})
	// on start: the owner's env token makes Inventory direct, the rest are Core by default
	n := lk.waitModes(t, "start", 0,
		"alice@example.com inventory direct", "alice@example.com split core", "alice@example.com habits core",
		"bob@example.com inventory core", "bob@example.com split core", "bob@example.com habits core")

	// a mode change and a pasted token (direct by default) are reported at once
	lk.ok(lk.aliceTok, "PUT", "/api/v1/settings", map[string]any{"habits_mode": "off"})
	lk.ok(lk.bobTok, "PUT", "/api/v1/settings", map[string]any{"split_url": lk.split.URL, "split_token": "bob-split"})
	n = lk.waitModes(t, "after change", n,
		"alice@example.com inventory direct", "alice@example.com split core", "alice@example.com habits off",
		"bob@example.com inventory core", "bob@example.com split direct", "bob@example.com habits core")

	// Disconnect (URL and token cleared) falls back to the default, Pocket Core
	lk.ok(lk.aliceTok, "PUT", "/api/v1/settings", map[string]any{"inventory_url": "", "inventory_token": ""})
	n = lk.waitModes(t, "after disconnect", n,
		"alice@example.com inventory core", "alice@example.com split core", "alice@example.com habits off",
		"bob@example.com inventory core", "bob@example.com split direct", "bob@example.com habits core")

	// settings that cannot change a mode do not report
	lk.ok(lk.aliceTok, "PUT", "/api/v1/settings", map[string]any{"default_servings": 3})
	time.Sleep(200 * time.Millisecond)
	if _, m := lk.core.reported(); m != n {
		t.Fatalf("reported on an unrelated change: %d → %d", n, m)
	}

	// what is reported is exactly what Settings shows
	for _, tok := range []string{lk.aliceTok, lk.bobTok} {
		st := lk.settings(tok)
		modes, _ := lk.s.connModes(context.Background())
		email := "alice@example.com"
		if tok == lk.bobTok {
			email = "bob@example.com"
		}
		for _, m := range modes {
			if m.User == email && st[m.Target+"_mode"] != m.Mode {
				t.Errorf("%s %s: settings %v, reported %s", email, m.Target, st[m.Target+"_mode"], m.Mode)
			}
		}
	}
}

// Without Pocket Core nothing is reported and nothing breaks.
func TestModesWithoutCore(t *testing.T) {
	cfg := testConfig(t.TempDir())
	s := New(cfg, openTestStore(t, cfg), slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if s.core.Configured() {
		t.Fatal("core configured without CORE_URL")
	}
	s.reportModes(context.Background())
	s.reportModesSoon()
}
