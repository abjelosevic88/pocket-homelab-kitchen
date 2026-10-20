package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
)

// sibling is a fake Pocket Inventory / Split / Habits: it records who called
// what, with which bearer token.
type sibling struct {
	*httptest.Server
	mu    sync.Mutex
	calls []string // "Bearer tok METHOD /path"
}

func newSibling(t *testing.T) *sibling {
	sb := &sibling{}
	sb.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sb.mu.Lock()
		sb.calls = append(sb.calls, r.Header.Get("Authorization")+" "+r.Method+" "+r.URL.Path)
		sb.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/items":
			io.WriteString(w, `[{"id":7,"name":"Flour","quantity":1000,"unit":"g","tags":"pantry"}]`)
		case strings.HasSuffix(r.URL.Path, "/adjust"):
			io.WriteString(w, `{"id":7,"name":"Flour","quantity":800,"unit":"g"}`)
		case strings.HasPrefix(r.URL.Path, "/api/v1/groups/"):
			io.WriteString(w, `{"id":1,"name":"Home","currency":"BAM","members":[{"id":1,"name":"A"}]}`)
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(sb.Close)
	return sb
}

func (sb *sibling) snapshot() []string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return append([]string(nil), sb.calls...)
}

// fakeCore answers introspection for paired-device tokens; nobody has an
// app link (Kitchen → Inventory / Split / Habits) here.
func newFakeCore(t *testing.T, tokens map[string]string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/links/token":
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"error":"no link","reason":"not_in_audience"}`)
			return
		case "/api/v1/links/status":
			io.WriteString(w, `[]`)
			return
		}
		if r.URL.Path == "/api/v1/auth/introspect" {
			var in struct {
				Token string `json:"token"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if email, ok := tokens[in.Token]; ok {
				fmt.Fprintf(w, `{"active":true,"kind":"device","user":{"id":9,"email":%q,"name":"x","role":"member"}}`, email)
				return
			}
			io.WriteString(w, `{"active":false}`)
			return
		}
		io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

type kitchen struct {
	t   *testing.T
	url string
	s   *Server
	st  *store.Store
}

// call sends a request as `auth` (a bearer token, or an *http.Client with a session cookie).
func (k *kitchen) call(auth any, method, path string, body any) (int, any) {
	k.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, k.url+path, rd)
	req.Header.Set("Content-Type", "application/json")
	cl := http.DefaultClient
	switch a := auth.(type) {
	case string:
		req.Header.Set("Authorization", "Bearer "+a)
	case *http.Client:
		cl = a
	}
	res, err := cl.Do(req)
	if err != nil {
		k.t.Fatal(err)
	}
	defer res.Body.Close()
	var out any
	raw, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

func (k *kitchen) ok(auth any, method, path string, body any) map[string]any {
	k.t.Helper()
	code, out := k.call(auth, method, path, body)
	if code >= 300 {
		k.t.Fatalf("%s %s → %d %v", method, path, code, out)
	}
	m, _ := out.(map[string]any)
	return m
}

func idOf(m map[string]any) int64 { v, _ := m["id"].(float64); return int64(v) }

func TestPerUserIsolation(t *testing.T) {
	sib := newSibling(t)
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.MaxUploadBytes = 1 << 20
	cfg.FoodFactsEnabled = false
	// the integration tokens in the environment are the OWNER's
	cfg.InventoryURL, cfg.InventoryToken, cfg.InventoryTag = sib.URL, "owner-inv", "pantry"
	cfg.HabitsURL, cfg.HabitsToken, cfg.HabitID = sib.URL, "owner-hab", 6
	cfg.SplitURL, cfg.SplitToken, cfg.SplitGroupID = sib.URL, "owner-split", 1
	cfg.CoreURL = newFakeCore(t, map[string]string{"pcd_alice": "alice@example.com", "pcd_bob": "bob@example.com", "pcd_carol": "carol@example.com"}).URL
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := openTestStore(t, cfg)
	ctx := context.Background()

	alice := &store.User{Email: "alice@example.com", Name: "Alice", Role: "admin"}
	if err := st.CreateUser(ctx, alice); err != nil {
		t.Fatal(err)
	}
	h, _ := bcrypt.GenerateFromPassword([]byte("bob-password"), bcrypt.MinCost)
	bob := &store.User{Email: "bob@example.com", Name: "Bob", Role: "member", PasswordHash: string(h)}
	if err := st.CreateUser(ctx, bob); err != nil {
		t.Fatal(err)
	}
	s := New(cfg, st, log, nil)
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	k := &kitchen{t: t, url: ts.URL, s: s, st: st}
	ta, _, _ := st.CreateAPIToken(ctx, alice.ID, "a")
	tb, _, _ := st.CreateAPIToken(ctx, bob.ID, "b")

	// ---- Alice's kitchen ----
	rc := k.ok(ta, "POST", "/api/v1/recipes", map[string]any{"title": "Secret stew", "ingredients": []map[string]any{{"name": "Flour", "qty": 200, "unit": "g"}}})
	rid := idOf(rc)
	pan := k.ok(ta, "POST", "/api/v1/pantry", map[string]any{"name": "Saffron", "quantity": 2, "unit": "g", "barcode": "4006381333931", "local": true})
	pid := idOf(pan)
	lists := k.ok(ta, "POST", "/api/v1/lists", map[string]any{"name": "Alice's list"})
	lid := idOf(lists)
	item := k.ok(ta, "POST", fmt.Sprintf("/api/v1/lists/%d/items", lid), map[string]any{"name": "2 kg potatoes"})
	iid := idOf(item)
	plan := k.ok(ta, "POST", "/api/v1/plan", map[string]any{"date": store.Today(), "slot": "dinner", "recipe_id": rid})
	planID := idOf(plan)
	before := len(sib.snapshot())
	cooked := k.ok(ta, "POST", fmt.Sprintf("/api/v1/recipes/%d/cook", rid), map[string]any{})
	if cooked["habit_marked"] != true {
		t.Errorf("owner's habit integration should still work: %v", cooked)
	}
	ownerCalls := sib.snapshot()[before:]
	if len(ownerCalls) == 0 || !strings.Contains(strings.Join(ownerCalls, "\n"), "Bearer owner-hab PUT /api/v1/habits/6/entries/") {
		t.Errorf("owner cook did not mark habit 6 with the owner's token: %v", ownerCalls)
	}
	cookID := int64(cooked["cook"].(map[string]any)["id"].(float64))
	k.ok(ta, "POST", fmt.Sprintf("/api/v1/lists/%d/items/%d/toggle", lid, iid), nil)
	co := k.ok(ta, "POST", fmt.Sprintf("/api/v1/lists/%d/checkout", lid), map[string]any{"amount": 1234, "currency": "BAM", "restock": false, "remove": false})
	purchaseID := int64(co["purchase"].(map[string]any)["id"].(float64))

	// ---- Bob, through every way he can authenticate ----
	jar, _ := cookiejar.New(nil)
	session := &http.Client{Jar: jar}
	if code, out := k.call(session, "POST", "/api/v1/auth/login", map[string]any{"email": "bob@example.com", "password": "bob-password"}); code != 200 {
		t.Fatalf("bob login: %d %v", code, out)
	}
	for name, bobAuth := range map[string]any{"api token": tb, "session": session, "core device token": "pcd_bob"} {
		t.Run(name, func(t *testing.T) {
			k.t = t
			defer func() { k.t = nil }()
			notFound := []struct{ method, path string }{
				{"GET", fmt.Sprintf("/api/v1/recipes/%d", rid)},
				{"PUT", fmt.Sprintf("/api/v1/recipes/%d", rid)},
				{"DELETE", fmt.Sprintf("/api/v1/recipes/%d", rid)},
				{"POST", fmt.Sprintf("/api/v1/recipes/%d/favorite", rid)},
				{"POST", fmt.Sprintf("/api/v1/recipes/%d/cook", rid)},
				{"POST", fmt.Sprintf("/api/v1/recipes/%d/shop", rid)},
				{"POST", fmt.Sprintf("/api/v1/recipes/%d/duplicate", rid)},
				{"GET", fmt.Sprintf("/api/v1/recipes/%d/photo", rid)},
				{"DELETE", fmt.Sprintf("/api/v1/recipes/%d/photo", rid)},
				{"GET", fmt.Sprintf("/api/v1/recipes/%d/export.md", rid)},
				{"PUT", fmt.Sprintf("/api/v1/plan/%d", planID)},
				{"DELETE", fmt.Sprintf("/api/v1/plan/%d", planID)},
				{"POST", fmt.Sprintf("/api/v1/plan/%d/cook", planID)},
				{"GET", fmt.Sprintf("/api/v1/lists/%d", lid)},
				{"PUT", fmt.Sprintf("/api/v1/lists/%d", lid)},
				{"POST", fmt.Sprintf("/api/v1/lists/%d/items", lid)},
				{"PUT", fmt.Sprintf("/api/v1/lists/%d/items/%d", lid, iid)},
				{"DELETE", fmt.Sprintf("/api/v1/lists/%d/items/%d", lid, iid)},
				{"POST", fmt.Sprintf("/api/v1/lists/%d/items/%d/toggle", lid, iid)},
				{"POST", fmt.Sprintf("/api/v1/lists/%d/check", lid)},
				{"POST", fmt.Sprintf("/api/v1/lists/%d/clear", lid)},
				{"POST", fmt.Sprintf("/api/v1/lists/%d/checkout", lid)},
				{"POST", fmt.Sprintf("/api/v1/lists/%d/low", lid)},
				{"DELETE", fmt.Sprintf("/api/v1/lists/%d", lid)},
				{"GET", fmt.Sprintf("/api/v1/pantry/%d", pid)},
				{"PUT", fmt.Sprintf("/api/v1/pantry/%d", pid)},
				{"POST", fmt.Sprintf("/api/v1/pantry/%d/adjust", pid)},
				{"DELETE", fmt.Sprintf("/api/v1/pantry/%d", pid)},
				{"DELETE", fmt.Sprintf("/api/v1/cooklog/%d", cookID)},
				{"DELETE", fmt.Sprintf("/api/v1/purchases/%d", purchaseID)},
			}
			for _, c := range notFound {
				body := map[string]any{"name": "pwned", "title": "pwned", "delta": -1, "checked": true, "amount": 1, "date": store.Today()}
				if code, out := k.call(bobAuth, c.method, c.path, body); code != http.StatusNotFound {
					t.Errorf("%s %s → %d %v, want 404", c.method, c.path, code, out)
				}
			}
			empty := func(path, key string) {
				t.Helper()
				code, out := k.call(bobAuth, "GET", path, nil)
				if code != 200 {
					t.Fatalf("GET %s → %d", path, code)
				}
				var list []any
				switch v := out.(type) {
				case []any:
					list = v
				case map[string]any:
					list, _ = v[key].([]any)
				}
				for _, x := range list {
					if strings.Contains(fmt.Sprint(x), "Secret stew") || strings.Contains(fmt.Sprint(x), "Saffron") || strings.Contains(fmt.Sprint(x), "Alice") || strings.Contains(fmt.Sprint(x), "potato") {
						t.Errorf("GET %s leaks Alice's data: %v", path, x)
					}
				}
			}
			empty("/api/v1/recipes", "")
			empty("/api/v1/recipes?archived=1", "")
			empty("/api/v1/search?q=secret", "recipes")
			empty("/api/v1/search?q=saffron", "pantry")
			empty("/api/v1/plan?from=2000-01-01&to=2100-01-01", "entries")
			empty("/api/v1/lists", "")
			empty("/api/v1/lists/suggest?q=pot", "")
			empty("/api/v1/pantry", "")
			empty("/api/v1/cooklog", "")
			empty("/api/v1/purchases", "")
			empty("/api/v1/users", "")
			for _, key := range []string{"recipes", "plan", "lists", "items", "pantry", "cooks", "purchases"} {
				empty("/api/v1/export.json", key)
			}
			for _, key := range []string{"week", "lists", "cooks", "suggestions", "expiring", "low"} {
				empty("/api/v1/home", key)
			}
			_, look := k.call(bobAuth, "GET", "/api/v1/lookup?barcode=4006381333931", nil)
			if m, _ := look.(map[string]any); m["source"] == "pantry" {
				t.Errorf("barcode lookup found Alice's pantry row: %v", m)
			}
			_, dash := k.call(bobAuth, "GET", "/api/v1/dashboard", nil)
			if m, _ := dash.(map[string]any); m["recipes"] != float64(0) || m["cooked_month"] != float64(0) || m["spend_month"] != float64(0) {
				t.Errorf("dashboard counts Alice's data: %v", m)
			}
			_, set := k.call(bobAuth, "GET", "/api/v1/settings", nil)
			if m, _ := set.(map[string]any); m["inventory_configured"] != false || m["habits_configured"] != false || m["split_configured"] != false || m["inventory_token"] != "" || m["habits_token"] != "" || m["split_token"] != "" || m["habit_id"] != float64(0) || m["split_group_id"] != float64(0) {
				t.Errorf("member sees the owner's integrations: %v", m)
			}
			if code, _ := k.call(bobAuth, "GET", "/api/v1/backup", nil); code != http.StatusForbidden {
				t.Errorf("member downloaded the whole database: %d", code)
			}
		})
	}
	k.t = t

	// Bob's own work never reaches Alice's Inventory / Split / Habits
	before = len(sib.snapshot())
	brc := k.ok(tb, "POST", "/api/v1/recipes", map[string]any{"title": "Bob's pancakes", "ingredients": []map[string]any{{"name": "Flour", "qty": 100, "unit": "g"}}})
	bcook := k.ok(tb, "POST", fmt.Sprintf("/api/v1/recipes/%d/cook", idOf(brc)), map[string]any{"plan_id": planID})
	if bcook["habit_marked"] == true {
		t.Errorf("member's cook marked the owner's habit")
	}
	if c := bcook["cook"].(map[string]any); c["plan_id"] != nil {
		t.Errorf("member's cook attached to the owner's plan entry: %v", c)
	}
	code, out := k.call(tb, "GET", "/api/v1/lists", nil)
	blists, _ := out.([]any)
	if code != 200 || len(blists) != 0 {
		t.Errorf("bob's lists: %d %v", code, out)
	}
	shop := k.ok(tb, "POST", fmt.Sprintf("/api/v1/recipes/%d/shop", idOf(brc)), map[string]any{})
	blid := int64(shop["list"].(map[string]any)["id"].(float64))
	if blid == lid {
		t.Fatal("bob's default list is Alice's list")
	}
	bitems := k.ok(tb, "GET", fmt.Sprintf("/api/v1/lists/%d", blid), nil)["items"].([]any)
	biid := idOf(bitems[0].(map[string]any))
	if code, _ := k.call(tb, "PUT", fmt.Sprintf("/api/v1/lists/%d/items/%d", blid, biid), map[string]any{"list_id": lid}); code != http.StatusNotFound {
		t.Errorf("bob moved his line onto Alice's list: %d", code)
	}
	if code, _ := k.call(tb, "POST", fmt.Sprintf("/api/v1/lists/%d/items", blid), map[string]any{"name": "x", "pantry_id": pid}); code != 201 {
		t.Errorf("create item: %d", code)
	} else if it := k.ok(tb, "GET", fmt.Sprintf("/api/v1/lists/%d", blid), nil)["items"].([]any); strings.Contains(fmt.Sprint(it), fmt.Sprintf("pantry_id:%d", pid)) {
		t.Errorf("bob's line points at Alice's pantry row: %v", it)
	}
	k.ok(tb, "POST", fmt.Sprintf("/api/v1/lists/%d/check", blid), map[string]any{"checked": true})
	bco := k.ok(tb, "POST", fmt.Sprintf("/api/v1/lists/%d/checkout", blid), map[string]any{"amount": 500, "currency": "BAM", "split": true})
	if !strings.Contains(fmt.Sprint(bco["warnings"]), "not set up") {
		t.Errorf("member checkout should not reach Split: %v", bco)
	}
	if code, _ := k.call(tb, "POST", "/api/v1/plan", map[string]any{"date": store.Today(), "recipe_id": rid}); code != http.StatusBadRequest {
		t.Errorf("bob planned Alice's recipe: %d", code)
	}
	if code, out := k.call(tb, "POST", "/api/v1/integrations/test", map[string]any{"which": "habits"}); code != 200 || out.(map[string]any)["ok"] == true {
		t.Errorf("member integration test used the owner's habits token: %v", out)
	}
	k.ok(tb, "POST", "/api/v1/pantry/sync", nil)
	for _, c := range sib.snapshot()[before:] {
		t.Errorf("member action called a sibling app: %s", c)
	}

	// settings: personal values per user, box values only from an admin
	k.ok(tb, "PUT", "/api/v1/settings", map[string]any{"title": "Bob's box", "week_start": "sun", "food_facts": true})
	if m := k.ok(ta, "GET", "/api/v1/settings", nil); m["week_start"] != "mon" || m["title"] != store.DefaultTitle || m["food_facts"] != false || m["habits_configured"] != true || m["habit_id"] != float64(6) {
		t.Errorf("alice's settings after bob saved his: %v", m)
	}
	if m := k.ok(tb, "GET", "/api/v1/settings", nil); m["week_start"] != "sun" || m["title"] != store.DefaultTitle {
		t.Errorf("bob's settings: %v", m)
	}

	// the background sync only uses the owner's token, into the owner's pantry
	before = len(sib.snapshot())
	s.autoSync(ctx)
	for _, c := range sib.snapshot()[before:] {
		if !strings.HasPrefix(c, "Bearer owner-inv ") {
			t.Errorf("background sync call with a foreign token: %s", c)
		}
	}
	if ps, _ := st.ListPantry(ctx, alice.ID, store.PantryFilter{Query: "flour"}); len(ps) != 1 {
		t.Errorf("owner's pantry after sync: %v", ps)
	}
	bps, _ := st.ListPantry(ctx, bob.ID, store.PantryFilter{})
	for _, p := range bps {
		if p.InventoryID != nil {
			t.Errorf("bob's pantry got a mirror of the owner's Inventory: %+v", p)
		}
	}

	// Alice still has everything, through her token and her Core device token
	for _, auth := range []string{ta, "pcd_alice"} {
		if m := k.ok(auth, "GET", fmt.Sprintf("/api/v1/recipes/%d", rid), nil); m["recipe"].(map[string]any)["title"] != "Secret stew" {
			t.Errorf("alice lost her recipe: %v", m)
		}
		if code, out := k.call(auth, "GET", "/api/v1/recipes", nil); code != 200 || strings.Contains(fmt.Sprint(out), "Bob's pancakes") {
			t.Errorf("alice sees bob's recipe: %v", out)
		}
	}
	if c := st.CountsFor(ctx, alice.ID); c["recipes"] != 1 || c["pantry"] != 2 || c["lists"] != 1 || c["cooklog"] != 1 || c["purchases"] != 1 || c["plan"] != 1 {
		t.Errorf("alice's counts: %v", c)
	}

	// a stranger arriving with a Core device token (Core says "member" here, but
	// even an admin claim would not matter) becomes a member, never an admin
	if code, _ := k.call("pcd_carol", "GET", "/api/v1/recipes", nil); code != 200 {
		t.Errorf("carol via core: %d", code)
	}
	if u, err := st.GetUserByEmail(ctx, "carol@example.com"); err != nil || u.Role != "member" {
		t.Errorf("auto-created core account: %+v %v", u, err)
	}
	if code, out := k.call("pcd_carol", "GET", "/api/v1/recipes", nil); strings.Contains(fmt.Sprint(out), "Secret stew") || code != 200 {
		t.Errorf("carol sees alice's recipes: %v", out)
	}

	// removing Bob removes his kitchen, not Alice's
	if code, _ := k.call(ta, "DELETE", fmt.Sprintf("/api/v1/users/%d", bob.ID), nil); code != http.StatusNoContent {
		t.Fatalf("delete bob: %d", code)
	}
	if c := st.CountsFor(ctx, bob.ID); c["recipes"]+c["lists"]+c["list_items"]+c["cooklog"]+c["purchases"] != 0 {
		t.Errorf("bob's rows left behind: %v", c)
	}
	if c := st.CountsFor(ctx, alice.ID); c["recipes"] != 1 || c["lists"] != 1 {
		t.Errorf("alice's rows after deleting bob: %v", c)
	}
}

// TestImportGoesToCaller: a member importing an export gets the rows in their
// own kitchen and the owner's kitchen is untouched.
func TestImportGoesToCaller(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := openTestStore(t, cfg)
	ctx := context.Background()
	a := &store.User{Email: "a@x", Role: "admin"}
	b := &store.User{Email: "b@x", Role: "member"}
	_ = st.CreateUser(ctx, a)
	_ = st.CreateUser(ctx, b)
	s := New(cfg, st, log, nil)
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	k := &kitchen{t: t, url: ts.URL, s: s, st: st}
	tb, _, _ := st.CreateAPIToken(ctx, b.ID, "b")
	k.ok(tb, "POST", "/api/v1/import", map[string]any{"recipes": []map[string]any{{"id": 1, "title": "Imported", "servings": 2}}, "lists": []map[string]any{{"id": 5, "name": "L"}}, "items": []map[string]any{{"list_id": 5, "name": "milk"}}})
	if c := st.CountsFor(ctx, b.ID); c["recipes"] != 1 || c["lists"] != 1 || c["list_items"] != 1 {
		t.Errorf("bob after import: %v", c)
	}
	if c := st.CountsFor(ctx, a.ID); c["recipes"] != 0 || c["lists"] != 0 {
		t.Errorf("alice after bob's import: %v", c)
	}
}
