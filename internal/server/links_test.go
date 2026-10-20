package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
)

// Pocket Day reading Bob's cook log through a Core app link: only
// cooklog:read routes (and /auth/me) open, only Bob's cooks visible.
func TestCoreLinkCooklogRead(t *testing.T) {
	// pcx token → scope; all act for Bob, from Pocket Day
	links := map[string]string{"pcx_day_bob": "cooklog:read", "pcx_other_bob": "something:else"}
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		scope, ok := links[in.Token]
		if !ok {
			io.WriteString(w, `{"active":false}`)
			return
		}
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"kitchen","user":{"id":2,"email":"bob@example.com","name":"Bob","role":"member"},"app":{"id":5,"slug":"day","name":"Pocket Day"}}`, scope)
	}))
	t.Cleanup(coreSrv.Close)

	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.FoodFactsEnabled = false
	cfg.CoreURL = coreSrv.URL
	st := openTestStore(t, cfg)
	ctx := context.Background()
	alice := &store.User{Email: "alice@example.com", Name: "Alice", Role: "admin"}
	bob := &store.User{Email: "bob@example.com", Name: "Bob", Role: "member"}
	_ = st.CreateUser(ctx, alice)
	_ = st.CreateUser(ctx, bob)
	s := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	k := &kitchen{t: t, url: ts.URL, s: s, st: st}
	ta, _, _ := st.CreateAPIToken(ctx, alice.ID, "a")
	tb, _, _ := st.CreateAPIToken(ctx, bob.ID, "b")

	arc := k.ok(ta, "POST", "/api/v1/recipes", map[string]any{"title": "Alice's secret stew"})
	k.ok(ta, "POST", fmt.Sprintf("/api/v1/recipes/%d/cook", idOf(arc)), map[string]any{})
	brc := k.ok(tb, "POST", "/api/v1/recipes", map[string]any{"title": "Bob's pancakes"})
	bcook := k.ok(tb, "POST", fmt.Sprintf("/api/v1/recipes/%d/cook", idOf(brc)), map[string]any{})
	bcookID := int64(bcook["cook"].(map[string]any)["id"].(float64))

	code, out := k.call("pcx_day_bob", "GET", "/api/v1/cooklog?limit=500", nil)
	if code != 200 || !strings.Contains(fmt.Sprint(out), "Bob's pancakes") || strings.Contains(fmt.Sprint(out), "Alice") {
		t.Fatalf("linked cook log %d %v", code, out)
	}
	if code, out := k.call("pcx_day_bob", "GET", "/api/v1/auth/me", nil); code != 200 || out.(map[string]any)["email"] != "bob@example.com" {
		t.Fatalf("auth/me %d %v", code, out)
	}
	// any linked app may ask who it acts for, but nothing outside its scope
	if code, _ := k.call("pcx_other_bob", "GET", "/api/v1/auth/me", nil); code != 200 {
		t.Fatalf("auth/me with another scope: %d", code)
	}
	if code, _ := k.call("pcx_other_bob", "GET", "/api/v1/cooklog", nil); code != 403 {
		t.Fatalf("cook log without cooklog:read: %d", code)
	}
	for _, c := range []struct{ m, p string }{
		{"DELETE", fmt.Sprintf("/api/v1/cooklog/%d", bcookID)},
		{"GET", "/api/v1/recipes"}, {"POST", "/api/v1/recipes"}, {"GET", fmt.Sprintf("/api/v1/recipes/%d", idOf(brc))},
		{"POST", fmt.Sprintf("/api/v1/recipes/%d/cook", idOf(brc))},
		{"GET", "/api/v1/pantry"}, {"GET", "/api/v1/lists"}, {"GET", "/api/v1/plan"}, {"GET", "/api/v1/purchases"},
		{"GET", "/api/v1/settings"}, {"PUT", "/api/v1/settings"}, {"GET", "/api/v1/tokens"}, {"POST", "/api/v1/tokens"},
		{"GET", "/api/v1/export.json"}, {"GET", "/api/v1/backup"}, {"GET", "/api/v1/home"},
	} {
		if code, _ := k.call("pcx_day_bob", c.m, c.p, map[string]any{}); code != 403 {
			t.Errorf("%s %s with a link token: %d, want 403", c.m, c.p, code)
		}
	}
	if code, _ := k.call("pcx_unknown", "GET", "/api/v1/cooklog", nil); code != 401 {
		t.Fatalf("unknown link token: %d", code)
	}
	if c := st.CountsFor(ctx, bob.ID); c["cooklog"] != 1 {
		t.Fatalf("bob's cook log changed: %v", c)
	}
}
