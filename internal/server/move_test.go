package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
)

// moveKitchen starts Kitchen behind a fake Core whose pcx_<who>_<export|import|read>
// tokens are Pocket Move links acting for alice@ or bob@. It returns the
// kitchen and Alice's and Bob's own API tokens and ids.
func moveKitchen(t *testing.T) (k *kitchen, ta, tb string, alice, bob *store.User) {
	t.Helper()
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
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		if len(parts) != 2 {
			io.WriteString(w, `{"active":false}`)
			return
		}
		scope := map[string]string{"export": core.ScopeMoveExport, "import": core.ScopeMoveImport, "read": "recipes:read"}[parts[1]]
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"kitchen","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"move","name":"Pocket Move"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	cfg := testConfig(t.TempDir())
	cfg.FoodFactsEnabled = false
	cfg.CoreURL = coreSrv.URL
	st := openTestStore(t, cfg)
	ctx := context.Background()
	alice = &store.User{Email: "alice@example.com", Name: "Alice", Role: "admin"}
	bob = &store.User{Email: "bob@example.com", Name: "Bob", Role: "member"}
	_ = st.CreateUser(ctx, alice)
	_ = st.CreateUser(ctx, bob)
	s := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	k = &kitchen{t: t, url: ts.URL, s: s, st: st}
	ta, _, _ = st.CreateAPIToken(ctx, alice.ID, "a")
	tb, _, _ = st.CreateAPIToken(ctx, bob.ID, "b")
	return
}

func (k *kitchen) raw(tok, method, path string, body []byte) (int, []byte) {
	k.t.Helper()
	req, _ := http.NewRequest(method, k.url+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		k.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func TestMoveLinkScopes(t *testing.T) {
	k, _, _, _, _ := moveKitchen(t)
	exp, imp, read := "pcx_bob_export", "pcx_bob_import", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.json", 200},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/recipes", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.json", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{imp, "POST", "/api/v1/recipes", 403},
		{imp, "POST", "/api/v1/recipes/import", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.json", 403},
		{read, "POST", "/api/v1/import", 403},
	} {
		if code, body := k.raw(c.tok, c.method, c.path, []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := k.raw(exp, "GET", core.PortablePath, nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 1 || len(p.Imports) != 1 || p.Exports[0].Kind != core.KindPocket || p.Imports[0].Form != "" {
		t.Fatalf("portable: %s", body)
	}
}

// Move brings Alice's kitchen to Bob: the import adds once (list items, plan,
// cooks and purchases included), and nothing of one person shows up for the other.
func TestMoveImportsAddOnce(t *testing.T) {
	k, ta, _, alice, bob := moveKitchen(t)
	ctx := context.Background()
	rid := idOf(k.ok(ta, "POST", "/api/v1/recipes", map[string]any{"title": "Secret stew", "ingredients": []map[string]any{{"name": "Flour", "qty": 200, "unit": "g"}}}))
	old := idOf(k.ok(ta, "POST", "/api/v1/recipes", map[string]any{"title": "Old soup"}))
	k.ok(ta, "PUT", fmt.Sprintf("/api/v1/recipes/%d", old), map[string]any{"archived": true})
	k.ok(ta, "POST", "/api/v1/pantry", map[string]any{"name": "Saffron", "quantity": 2, "unit": "g", "local": true})
	lid := idOf(k.ok(ta, "POST", "/api/v1/lists", map[string]any{"name": "Alice's list"}))
	iid := idOf(k.ok(ta, "POST", fmt.Sprintf("/api/v1/lists/%d/items", lid), map[string]any{"name": "potatoes"}))
	k.ok(ta, "POST", fmt.Sprintf("/api/v1/lists/%d/items", lid), map[string]any{"name": "milk"})
	k.ok(ta, "POST", "/api/v1/plan", map[string]any{"date": store.Today(), "slot": "dinner", "recipe_id": rid})
	k.ok(ta, "POST", fmt.Sprintf("/api/v1/recipes/%d/cook", rid), map[string]any{})
	k.ok(ta, "POST", fmt.Sprintf("/api/v1/lists/%d/items/%d/toggle", lid, iid), nil)
	k.ok(ta, "POST", fmt.Sprintf("/api/v1/lists/%d/checkout", lid), map[string]any{"amount": 1234, "currency": "BAM", "restock": false, "remove": false})

	code, export := k.raw("pcx_alice_export", "GET", "/api/v1/export.json", nil)
	if code != 200 || !strings.Contains(string(export), "Secret stew") || !strings.Contains(string(export), "Old soup") {
		t.Fatalf("alice's export: %d %s", code, export)
	}
	if _, b := k.raw("pcx_bob_export", "GET", "/api/v1/export.json", nil); strings.Contains(string(b), "Secret stew") || strings.Contains(string(b), "Saffron") {
		t.Fatalf("bob's export holds alice's kitchen: %s", b)
	}
	aliceBefore := k.st.CountsFor(ctx, alice.ID)

	imp := func() map[string]any {
		code, out := k.raw("pcx_bob_import", "POST", "/api/v1/import", export)
		if code != 200 {
			t.Fatalf("import → %d %s", code, out)
		}
		var m map[string]any
		_ = json.Unmarshal(out, &m)
		return m
	}
	first := imp()
	if num(first, "recipes") != 2 || num(first, "pantry") != 1 || num(first, "lists") != 1 || num(first, "plan") != 1 || num(first, "cooks") != 1 || num(first, "purchases") != 1 {
		t.Fatalf("first import: %v", first)
	}
	bobAfter := k.st.CountsFor(ctx, bob.ID)
	if second := imp(); len(second) != 0 {
		t.Fatalf("second import added: %v", second)
	}
	if again := k.st.CountsFor(ctx, bob.ID); !maps.Equal(again, bobAfter) {
		t.Fatalf("bob's kitchen grew on the second import: %v → %v", bobAfter, again)
	}
	if c := k.st.CountsFor(ctx, alice.ID); !maps.Equal(c, aliceBefore) {
		t.Fatalf("alice's kitchen changed: %v → %v", aliceBefore, c)
	}
}

func num(m map[string]any, k string) float64 { v, _ := m[k].(float64); return v }
