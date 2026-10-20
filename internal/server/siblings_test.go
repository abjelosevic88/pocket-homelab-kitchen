package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/config"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
)

// linkCore is a fake Pocket Core for app links: it hands out pcx_ tokens
// that spell out app, scopes and person, and lets a test switch links off.
type linkCore struct {
	*httptest.Server
	mu     sync.Mutex
	urls   map[string]string   // app slug → URL
	allow  map[string]bool     // emails Core knows
	off    map[string]bool     // "app scope" switched off
	issued []string            // "app scopes user"
	modes  []map[string]string // the last PUT /api/v1/links/modes
	nModes int                 // how many reports came in
}

func newLinkCore(t *testing.T) *linkCore {
	c := &linkCore{urls: map[string]string{}, allow: map[string]bool{}, off: map[string]bool{}}
	c.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		c.mu.Lock()
		defer c.mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/apps/register":
			io.WriteString(w, `{"app":{},"token":"pca_test"}`)
		case "/api/v1/links/modes":
			var in struct {
				Modes []map[string]string `json:"modes"`
			}
			if r.Method != http.MethodPut || r.Header.Get("Authorization") != "Bearer pca_test" || json.NewDecoder(r.Body).Decode(&in) != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			c.modes, c.nModes = in.Modes, c.nModes+1
			fmt.Fprintf(w, `{"recorded":%d}`, len(in.Modes))
		case "/api/v1/links/status":
			app, user := r.URL.Query().Get("app"), r.URL.Query().Get("user")
			out := []map[string]any{}
			for _, a := range pocketApps {
				if a.slug != app {
					continue
				}
				for _, sc := range a.scopes {
					state, on := "on", true
					if !c.allow[user] {
						state, on = "not_in_audience", false
					} else if c.off[app+" "+sc] {
						state, on = "off_person", false
					}
					out = append(out, map[string]any{"app": app, "scope": sc, "state": state, "on": on})
				}
			}
			_ = json.NewEncoder(w).Encode(out)
		case "/api/v1/links/token":
			var in struct {
				User   string   `json:"user"`
				App    string   `json:"app"`
				Scopes []string `json:"scopes"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			sort.Strings(in.Scopes)
			deny := func(reason string) {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprintf(w, `{"error":"no","reason":%q}`, reason)
			}
			if !c.allow[in.User] {
				deny("not_in_audience")
				return
			}
			for _, sc := range in.Scopes {
				if c.off[in.App+" "+sc] {
					deny("off_person")
					return
				}
			}
			c.issued = append(c.issued, in.App+" "+strings.Join(in.Scopes, "+")+" "+in.User)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "pcx_" + in.App + "_" + strings.Join(in.Scopes, "+") + "_" + in.User, "expires_in": 300,
				"url": c.urls[in.App], "public_url": "https://" + in.App + ".example"})
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(c.Close)
	return c
}

func (c *linkCore) take() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.issued
	c.issued = nil
	return out
}

func (c *linkCore) set(key string, off bool) {
	c.mu.Lock()
	c.off[key] = off
	c.mu.Unlock()
}

// fakeApp is a fake Pocket Inventory / Split / Habits recording "token METHOD path".
type fakeApp struct {
	*httptest.Server
	mu    sync.Mutex
	calls []string
}

func newFakeApp(t *testing.T) *fakeApp {
	a := &fakeApp{}
	a.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.calls = append(a.calls, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")+" "+r.Method+" "+r.URL.Path)
		a.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case p == "/healthz":
			io.WriteString(w, `{"status":"ok","version":"9.9"}`)
		case p == "/api/v1/items" && r.Method == "GET":
			io.WriteString(w, `[{"id":7,"name":"Flour","quantity":1000,"unit":"g","tags":"pantry","photo":"x.jpg"}]`)
		case p == "/api/v1/items" && r.Method == "POST":
			io.WriteString(w, `{"id":8,"name":"Rice","quantity":1,"unit":"kg"}`)
		case strings.HasSuffix(p, "/adjust"):
			io.WriteString(w, `{"id":7,"name":"Flour","quantity":800,"unit":"g"}`)
		case p == "/api/v1/locations":
			io.WriteString(w, `[{"id":1,"name":"Kitchen"}]`)
		case p == "/api/v1/groups":
			io.WriteString(w, `[{"id":1,"name":"Home","currency":"BAM"}]`)
		case strings.HasPrefix(p, "/api/v1/groups/") && strings.HasSuffix(p, "/expenses"):
			io.WriteString(w, `{"id":55,"group_id":1}`)
		case strings.HasPrefix(p, "/api/v1/groups/"):
			io.WriteString(w, `{"id":1,"name":"Home","currency":"BAM","members":[{"id":3,"name":"Alice"}]}`)
		case p == "/api/v1/categories":
			io.WriteString(w, `["groceries"]`)
		case p == "/api/v1/habits":
			io.WriteString(w, `[{"id":6,"name":"Cook at home"}]`)
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(a.Close)
	return a
}

func (a *fakeApp) take() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.calls
	a.calls = nil
	return out
}

type linkKitchen struct {
	*kitchen
	core               *linkCore
	inv, split, habits *fakeApp
	alice, bob         *store.User
	aliceTok, bobTok   string
	cfg                *config.Config
}

func newLinkKitchen(t *testing.T, env func(*config.Config, *linkKitchen)) *linkKitchen {
	lk := &linkKitchen{core: newLinkCore(t), inv: newFakeApp(t), split: newFakeApp(t), habits: newFakeApp(t)}
	lk.core.urls = map[string]string{"inventory": lk.inv.URL, "split": lk.split.URL, "habits": lk.habits.URL}
	lk.core.allow["alice@example.com"] = true
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.FoodFactsEnabled = false
	cfg.CoreURL = lk.core.URL
	cfg.CoreRegistrationKey = "pcr_test"
	if env != nil {
		env(cfg, lk)
	}
	lk.cfg = cfg
	st := openTestStore(t, cfg)
	ctx := t.Context()
	lk.alice = &store.User{Email: "alice@example.com", Name: "Alice", Role: "admin"}
	lk.bob = &store.User{Email: "bob@example.com", Name: "Bob", Role: "member"}
	_ = st.CreateUser(ctx, lk.alice)
	_ = st.CreateUser(ctx, lk.bob)
	s := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	lk.kitchen = &kitchen{t: t, url: ts.URL, s: s, st: st}
	lk.aliceTok, _, _ = st.CreateAPIToken(ctx, lk.alice.ID, "a")
	lk.bobTok, _, _ = st.CreateAPIToken(ctx, lk.bob.ID, "b")
	return lk
}

func (lk *linkKitchen) settings(tok string) map[string]any {
	return lk.ok(tok, "GET", "/api/v1/settings", nil)
}

func (lk *linkKitchen) drain() {
	lk.core.take()
	lk.inv.take()
	lk.split.take()
	lk.habits.take()
}

func want(t *testing.T, what string, got []string, exp ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(exp, "\n") {
		t.Errorf("%s:\n got  %q\n want %q", what, got, exp)
	}
}

// Through Pocket Core: every call carries a pcx_ token Core issued for the
// person with just the scopes that call needs.
func TestActingThroughCore(t *testing.T) {
	lk := newLinkKitchen(t, nil)
	a := lk.aliceTok

	st := lk.settings(a)
	for _, app := range []string{"inventory", "split", "habits"} {
		if st[app+"_mode"] != "core" || st[app+"_via"] != "core" || st[app+"_detail"] != "" {
			t.Fatalf("%s: %v", app, st)
		}
	}
	if st["inventory_configured"] != true || st["split_configured"] != false || st["habits_configured"] != false || st["core_configured"] != true {
		t.Fatalf("configured flags before ids are picked: %v", st)
	}

	// pickers list through the link, read scopes only
	lk.drain()
	_, groups := lk.call(a, "GET", "/api/v1/split/groups", nil)
	_, habits := lk.call(a, "GET", "/api/v1/habits/list", nil)
	_, locs := lk.call(a, "GET", "/api/v1/inventory/locations", nil)
	if !strings.Contains(fmt.Sprint(groups), "Home") || !strings.Contains(fmt.Sprint(habits), "Cook at home") || !strings.Contains(fmt.Sprint(locs), "Kitchen") {
		t.Fatalf("pickers: %v %v %v", groups, habits, locs)
	}
	for _, c := range lk.core.take() {
		if !strings.HasPrefix(c, "split groups:read alice") && !strings.HasPrefix(c, "habits habits:read alice") && !strings.HasPrefix(c, "inventory items:read alice") {
			t.Errorf("picker asked Core for %q", c)
		}
	}
	lk.ok(a, "PUT", "/api/v1/settings", map[string]any{"split_group_id": 1, "habit_id": 6})
	if st := lk.settings(a); st["split_configured"] != true || st["habits_configured"] != true {
		t.Fatalf("after picking: %v", st)
	}

	// pantry sync reads through Core; photos point at the public Inventory
	lk.drain()
	if out := lk.ok(a, "POST", "/api/v1/pantry/sync", nil); out["ok"] != true {
		t.Fatalf("sync %v", out)
	}
	want(t, "sync core", lk.core.take(), "inventory items:read alice@example.com")
	want(t, "sync inventory", lk.inv.take(), "pcx_inventory_items:read_alice@example.com GET /api/v1/items")
	ps, _ := lk.st.ListPantry(context.Background(), lk.alice.ID, store.PantryFilter{Query: "flour"})
	if len(ps) != 1 || ps[0].PhotoURL != "https://inventory.example/api/v1/items/7/photo" {
		t.Fatalf("mirror %+v", ps)
	}

	// a new pantry row is created in Inventory with items:write
	lk.ok(a, "POST", "/api/v1/pantry", map[string]any{"name": "Rice", "quantity": 1, "unit": "kg"})
	want(t, "create core", lk.core.take(), "inventory items:write alice@example.com", "inventory items:write alice@example.com")
	want(t, "create inventory", lk.inv.take(), "pcx_inventory_items:write_alice@example.com POST /api/v1/items")

	// cooking: one adjust token for the ingredients, entries:write for the habit
	rc := lk.ok(a, "POST", "/api/v1/recipes", map[string]any{"title": "Bread", "ingredients": []map[string]any{{"name": "Flour", "qty": 200, "unit": "g"}}})
	cooked := lk.ok(a, "POST", fmt.Sprintf("/api/v1/recipes/%d/cook", idOf(rc)), map[string]any{})
	if cooked["habit_marked"] != true || len(cooked["deducted"].([]any)) != 1 {
		t.Fatalf("cook %v", cooked)
	}
	want(t, "cook core", lk.core.take(), "inventory items:adjust alice@example.com", "habits entries:write alice@example.com")
	want(t, "cook inventory", lk.inv.take(), "pcx_inventory_items:adjust_alice@example.com POST /api/v1/items/7/adjust")
	want(t, "cook habits", lk.habits.take(), "pcx_habits_entries:write_alice@example.com PUT /api/v1/habits/6/entries/"+store.Today())

	// checkout posts the expense with groups:read + expenses:write, linking the public Split
	l := lk.ok(a, "POST", "/api/v1/lists", map[string]any{"name": "Shop"})
	lk.ok(a, "POST", fmt.Sprintf("/api/v1/lists/%d/items", idOf(l)), map[string]any{"name": "milk"})
	lk.ok(a, "POST", fmt.Sprintf("/api/v1/lists/%d/check", idOf(l)), map[string]any{"checked": true})
	lk.drain()
	co := lk.ok(a, "POST", fmt.Sprintf("/api/v1/lists/%d/checkout", idOf(l)), map[string]any{"amount": 900, "currency": "BAM", "split": true, "restock": false})
	if co["split_url"] != "https://split.example/groups/1" || co["purchase"].(map[string]any)["split_expense_id"] != float64(55) {
		t.Fatalf("checkout %v", co)
	}
	want(t, "checkout core", lk.core.take(), "split expenses:write+groups:read alice@example.com", "split expenses:write+groups:read alice@example.com")
	want(t, "checkout split", lk.split.take(), "pcx_split_expenses:write+groups:read_alice@example.com GET /api/v1/groups/1",
		"pcx_split_expenses:write+groups:read_alice@example.com POST /api/v1/groups/1/expenses")

	// test connection follows the mode
	if out := lk.ok(a, "POST", "/api/v1/integrations/test", map[string]any{"which": "habits"}); out["ok"] != true || !strings.Contains(out["message"].(string), "through Pocket Core") {
		t.Fatalf("test %v", out)
	}

	// background sync acts for each person: Alice through Core, Bob (no link) skipped
	lk.drain()
	lk.s.autoSync(context.Background())
	want(t, "auto sync core", lk.core.take(), "inventory items:read alice@example.com")
	want(t, "auto sync inventory", lk.inv.take(), "pcx_inventory_items:read_alice@example.com GET /api/v1/items")

	// Bob is not in Core's audience: not connected, nothing reaches the apps
	bs := lk.settings(lk.bobTok)
	if bs["inventory_mode"] != "core" || bs["inventory_via"] != "" || !strings.Contains(bs["inventory_detail"].(string), "do not have access to Pocket Inventory") || bs["inventory_configured"] != false {
		t.Fatalf("bob %v", bs)
	}
	brc := lk.ok(lk.bobTok, "POST", "/api/v1/recipes", map[string]any{"title": "Toast"})
	lk.ok(lk.bobTok, "POST", "/api/v1/pantry", map[string]any{"name": "Butter"})
	if c := lk.ok(lk.bobTok, "POST", fmt.Sprintf("/api/v1/recipes/%d/cook", idOf(brc)), map[string]any{}); c["habit_marked"] == true {
		t.Fatalf("bob %v", c)
	}
	for _, f := range []*fakeApp{lk.inv, lk.split, lk.habits} {
		want(t, "bob's calls", f.take())
	}
}

// Switching a link off in Core shows up at once, and only the work that
// needs that scope stops.
func TestActingLinkSwitchedOff(t *testing.T) {
	lk := newLinkKitchen(t, nil)
	a := lk.aliceTok
	lk.ok(a, "PUT", "/api/v1/settings", map[string]any{"habit_id": 6})
	lk.ok(a, "POST", "/api/v1/pantry/sync", nil)
	rc := lk.ok(a, "POST", "/api/v1/recipes", map[string]any{"title": "Bread", "ingredients": []map[string]any{{"name": "Flour", "qty": 200, "unit": "g"}}})
	cook := fmt.Sprintf("/api/v1/recipes/%d/cook", idOf(rc))

	lk.core.set("habits entries:write", true)
	lk.core.set("habits habits:read", true)
	st := lk.settings(a)
	if st["habits_via"] != "" || !strings.Contains(st["habits_detail"].(string), "switched the Kitchen → Pocket Habits link off") || st["habits_configured"] != false {
		t.Fatalf("habits off: %v", st)
	}
	// items:write off: the connection is partly on, adjusting still works
	lk.core.set("inventory items:write", true)
	st = lk.settings(a)
	if st["inventory_via"] != "core" || !strings.Contains(st["inventory_detail"].(string), "partly: items:write off") {
		t.Fatalf("inventory partly: %v", st)
	}
	lk.drain()
	cooked := lk.ok(a, "POST", cook, map[string]any{})
	if cooked["habit_marked"] == true || len(cooked["deducted"].([]any)) != 1 || len(cooked["warnings"].([]any)) != 0 {
		t.Fatalf("cook with habits off: %v", cooked)
	}
	want(t, "inventory still adjusted", lk.inv.take(), "pcx_inventory_items:adjust_alice@example.com POST /api/v1/items/7/adjust")
	want(t, "no habit call", lk.habits.take())
	// … but a row cannot be created in Inventory any more: kept local, says why
	if code, out := lk.call(a, "POST", "/api/v1/pantry", map[string]any{"name": "Salt"}); code != 201 || out.(map[string]any)["inventory_id"] != nil {
		t.Fatalf("create with items:write off: %d %v", code, out)
	}
	want(t, "no create call", lk.inv.take())
	if out := lk.ok(a, "POST", "/api/v1/integrations/test", map[string]any{"which": "habits"}); out["ok"] == true || !strings.Contains(out["error"].(string), "switched the Kitchen → Pocket Habits link off") {
		t.Fatalf("test with link off %v", out)
	}
	// back on: works again with the next request
	lk.core.set("habits entries:write", false)
	if c := lk.ok(a, "POST", cook, map[string]any{}); c["habit_marked"] != true {
		t.Fatalf("habit back on %v", c)
	}
}

// Direct uses the saved token, Off makes no calls at all, Disconnect clears
// URL and token.
func TestActingDirectAndOff(t *testing.T) {
	lk := newLinkKitchen(t, nil)
	a := lk.aliceTok
	rc := lk.ok(a, "POST", "/api/v1/recipes", map[string]any{"title": "Bread"})
	cook := fmt.Sprintf("/api/v1/recipes/%d/cook", idOf(rc))

	// a saved token makes it direct (how it always worked)
	lk.ok(a, "PUT", "/api/v1/settings", map[string]any{"habits_url": lk.habits.URL, "habits_token": "manual-habits", "habit_id": 6})
	st := lk.settings(a)
	if st["habits_mode"] != "direct" || st["habits_via"] != "direct" || st["habits_token"] != "manu••••bits" {
		t.Fatalf("direct %v", st)
	}
	lk.drain()
	if c := lk.ok(a, "POST", cook, map[string]any{}); c["habit_marked"] != true {
		t.Fatalf("direct cook %v", c)
	}
	want(t, "direct core", lk.core.take())
	want(t, "direct habits", lk.habits.take(), "manual-habits PUT /api/v1/habits/6/entries/"+store.Today())

	// choosing Pocket Core ignores the saved token (kept for switching back)
	lk.ok(a, "PUT", "/api/v1/settings", map[string]any{"habits_mode": "core"})
	lk.ok(a, "POST", cook, map[string]any{})
	want(t, "core again", lk.habits.take(), "pcx_habits_entries:write_alice@example.com PUT /api/v1/habits/6/entries/"+store.Today())

	// off: nothing at all, and settings say so
	lk.ok(a, "PUT", "/api/v1/settings", map[string]any{"habits_mode": "off", "inventory_mode": "off", "split_mode": "off", "split_group_id": 1})
	lk.drain()
	if c := lk.ok(a, "POST", cook, map[string]any{}); c["habit_marked"] == true {
		t.Fatalf("off cook %v", c)
	}
	lk.ok(a, "POST", "/api/v1/pantry", map[string]any{"name": "Rice"})
	lk.ok(a, "POST", "/api/v1/pantry/sync", nil)
	lk.ok(a, "GET", "/api/v1/split/groups", nil)
	lk.ok(a, "GET", "/api/v1/lookup?barcode=123", nil)
	l := lk.ok(a, "POST", "/api/v1/lists", map[string]any{"name": "Shop"})
	lk.ok(a, "POST", fmt.Sprintf("/api/v1/lists/%d/items", idOf(l)), map[string]any{"name": "milk"})
	lk.ok(a, "POST", fmt.Sprintf("/api/v1/lists/%d/check", idOf(l)), map[string]any{"checked": true})
	co := lk.ok(a, "POST", fmt.Sprintf("/api/v1/lists/%d/checkout", idOf(l)), map[string]any{"amount": 100, "currency": "BAM", "split": true})
	if !strings.Contains(fmt.Sprint(co["warnings"]), "Pocket Split is switched off") {
		t.Fatalf("off checkout %v", co)
	}
	lk.s.autoSync(context.Background())
	want(t, "off core", lk.core.take())
	for _, f := range []*fakeApp{lk.inv, lk.split, lk.habits} {
		want(t, "off calls", f.take())
	}
	st = lk.settings(a)
	if st["habits_mode"] != "off" || st["habits_via"] != "" || st["habits_detail"] != "switched off" || st["habits_token"] == "" || st["habits_configured"] != false {
		t.Fatalf("off settings %v", st)
	}

	// direct without a token says what is missing; Disconnect clears URL and token
	lk.ok(a, "PUT", "/api/v1/settings", map[string]any{"habits_mode": "direct", "habits_url": "", "habits_token": ""})
	st = lk.settings(a)
	if st["habits_token"] != "" || st["habits_url"] != "" || st["habits_via"] != "" || st["habits_detail"] != "no token yet" {
		t.Fatalf("disconnected %v", st)
	}
	if out := lk.ok(a, "POST", "/api/v1/integrations/test", map[string]any{"which": "habits"}); out["ok"] == true || !strings.Contains(out["error"].(string), "not set up") {
		t.Fatalf("test without token %v", out)
	}
}

// Tokens from the environment are the owner's and copied once: a token the
// owner cleared stays cleared after a restart, and the owner's pasted tokens
// keep working (direct) until they switch.
func TestEnvTokensSeededOnce(t *testing.T) {
	lk := newLinkKitchen(t, func(cfg *config.Config, lk *linkKitchen) {
		cfg.InventoryURL, cfg.InventoryToken = lk.inv.URL, "env-inv"
		cfg.HabitsURL, cfg.HabitsToken, cfg.HabitID = lk.habits.URL, "env-hab", 6
	})
	a := lk.aliceTok
	st := lk.settings(a)
	if st["inventory_mode"] != "direct" || st["habits_mode"] != "direct" || st["split_mode"] != "core" || st["habit_id"] != float64(6) {
		t.Fatalf("seeded %v", st)
	}
	if bs := lk.settings(lk.bobTok); bs["habits_token"] != "" || bs["habit_id"] != float64(0) || bs["habits_mode"] != "core" {
		t.Fatalf("member got the owner's env tokens %v", bs)
	}
	lk.ok(a, "PUT", "/api/v1/settings", map[string]any{"habits_url": "", "habits_token": "", "habit_id": 0, "habits_mode": "core"})
	if err := lk.s.ownerReady(context.Background(), lk.alice.ID); err != nil {
		t.Fatal(err)
	}
	st = lk.settings(a)
	if st["habits_token"] != "" || st["habits_url"] != "" || st["habit_id"] != float64(0) || st["habits_mode"] != "core" || st["inventory_token"] == "" {
		t.Fatalf("cleared token came back: %v", st)
	}
}

func TestManifestDeclaresLinks(t *testing.T) {
	uses := linkUses()
	got := []string{}
	for _, u := range uses {
		got = append(got, u.App+" "+strings.Join(u.Scopes, ","))
	}
	want(t, "uses", got, "inventory items:read,items:write,items:adjust", "split groups:read,expenses:write", "habits habits:read,entries:write")
	if len(linkOffers) != 1 || linkOffers[0].Scope != "cooklog:read" {
		t.Fatalf("offers %v", linkOffers)
	}
}
