package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/config"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// The receiver applies a write, then loses its first acknowledgement. Retrying
// after a new Server instance must carry the same key and frozen request.
func TestWorkflowRestartsAfterLostAcknowledgement(t *testing.T) {
	var mu sync.Mutex
	receipts := map[string]string{}
	bodies := map[string]string{}
	mutations := 0
	quantity := 1000.0
	inv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			t.Error("missing operation key")
			http.Error(w, "key required", 400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if saved, ok := receipts[key]; ok {
			if bodies[key] != string(raw) {
				t.Error("retry body changed")
			}
			io.WriteString(w, saved)
			return
		}
		var in struct{ Delta float64 }
		json.Unmarshal(raw, &in)
		quantity += in.Delta
		mutations++
		body := fmt.Sprintf(`{"id":7,"quantity":%g,"unit":"g"}`, quantity)
		receipts[key] = body
		bodies[key] = string(raw)
		http.Error(w, "acknowledgement lost", 503)
	}))
	defer inv.Close()
	lk := newLinkKitchen(t, func(cfg *config.Config, _ *linkKitchen) {
		cfg.InventoryURL = inv.URL
		cfg.InventoryToken = "direct-test"
	})
	p := lk.ok(lk.aliceTok, "POST", "/api/v1/pantry", map[string]any{"name": "Flour", "quantity": 1000, "unit": "g", "local": true})
	pid := idOf(p)
	if _, err := lk.st.DB.Exec(`UPDATE pantry SET inventory_id=7 WHERE id=?`, pid); err != nil {
		t.Fatal(err)
	}
	rc := lk.ok(lk.aliceTok, "POST", "/api/v1/recipes", map[string]any{"title": "Bread", "servings": 1, "ingredients": []map[string]any{{"name": "Flour", "qty": 200, "unit": "g", "pantry_id": pid}}})
	path := fmt.Sprintf("/api/v1/recipes/%d/cook", idOf(rc))
	body := `{"mark_habit":false}`
	call := func() map[string]any {
		req, _ := http.NewRequest("POST", lk.url+path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+lk.aliceTok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "cook-retry")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode >= 300 {
			t.Fatal(res.StatusCode, out)
		}
		return out
	}
	first := call()
	if len(first["warnings"].([]any)) == 0 {
		t.Fatal("pending action not reported")
	}
	var cooks int
	lk.st.DB.QueryRow(`SELECT COUNT(*) FROM cooklog`).Scan(&cooks)
	if cooks != 1 {
		t.Fatal(cooks)
	}
	restarted := New(lk.cfg, lk.st, lk.s.log, nil)
	if _, err := lk.st.DB.Exec(`CREATE TRIGGER reject_event BEFORE INSERT ON pocket_outbox BEGIN SELECT RAISE(ABORT,'outbox unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	lk.st.DB.Exec(`UPDATE pocket_actions SET next_at=0`)
	restarted.drainActions(t.Context(), lk.alice.ID)
	var pending int
	lk.st.DB.QueryRow(`SELECT COUNT(*) FROM pocket_actions WHERE done_at=0`).Scan(&pending)
	if pending != 1 {
		t.Fatal("completion survived missing event", pending)
	}
	lk.st.DB.Exec(`DROP TRIGGER reject_event`)
	lk.st.DB.Exec(`UPDATE pocket_actions SET next_at=0`)
	restarted.drainActions(t.Context(), lk.alice.ID)
	replay := call()
	if len(replay["warnings"].([]any)) != 0 || len(replay["deducted"].([]any)) != 1 {
		t.Fatal("replay", replay)
	}
	mu.Lock()
	defer mu.Unlock()
	if mutations != 1 || quantity != 800 {
		t.Fatal("duplicate inventory movement", mutations, quantity)
	}
	lk.st.DB.QueryRow(`SELECT COUNT(*) FROM cooklog`).Scan(&cooks)
	if cooks != 1 {
		t.Fatal("duplicate cook", cooks)
	}
	var local float64
	lk.st.DB.QueryRow(`SELECT quantity FROM pantry WHERE id=?`, pid).Scan(&local)
	if local != 800 {
		t.Fatal("local projection", local)
	}
}

func TestCheckoutRestartsAfterSplitLosesAcknowledgement(t *testing.T) {
	var mu sync.Mutex
	writes := 0
	keySaved, bodySaved := "", ""
	split := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			io.WriteString(w, `{"id":1,"name":"Home","currency":"EUR","members":[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		key := r.Header.Get("Idempotency-Key")
		mu.Lock()
		defer mu.Unlock()
		if key == "" {
			t.Error("missing split key")
		}
		if keySaved == "" {
			keySaved = key
			bodySaved = string(raw)
			writes++
			http.Error(w, "acknowledgement lost", 503)
			return
		}
		if key != keySaved || string(raw) != bodySaved {
			t.Error("split retry changed")
		}
		io.WriteString(w, `{"id":55}`)
	}))
	defer split.Close()
	lk := newLinkKitchen(t, func(cfg *config.Config, _ *linkKitchen) { cfg.SplitURL = split.URL; cfg.SplitToken = "direct-test" })
	lk.ok(lk.aliceTok, "PUT", "/api/v1/settings", map[string]any{"split_group_id": 1, "split_member_id": 1})
	list := lk.ok(lk.aliceTok, "POST", "/api/v1/lists", map[string]any{"name": "Shop"})
	lk.ok(lk.aliceTok, "POST", fmt.Sprintf("/api/v1/lists/%d/items", idOf(list)), map[string]any{"name": "Milk"})
	lk.ok(lk.aliceTok, "POST", fmt.Sprintf("/api/v1/lists/%d/check", idOf(list)), map[string]any{"checked": true})
	path := fmt.Sprintf("/api/v1/lists/%d/checkout", idOf(list))
	call := func() map[string]any {
		req, _ := http.NewRequest("POST", lk.url+path, bytes.NewBufferString(`{"amount":900,"currency":"EUR","split":true,"restock":false}`))
		req.Header.Set("Authorization", "Bearer "+lk.aliceTok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "checkout-retry")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode >= 300 {
			t.Fatal(res.StatusCode, out)
		}
		return out
	}
	first := call()
	if len(first["warnings"].([]any)) == 0 {
		t.Fatal("pending split not reported")
	}
	lk.st.DB.Exec(`UPDATE pocket_actions SET next_at=0`)
	restarted := New(lk.cfg, lk.st, lk.s.log, nil)
	restarted.drainActions(t.Context(), lk.alice.ID)
	replay := call()
	if replay["purchase"].(map[string]any)["split_expense_id"] != float64(55) {
		t.Fatal(replay)
	}
	var count int
	lk.st.DB.QueryRow(`SELECT COUNT(*) FROM purchases`).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate purchase", count)
	}
	mu.Lock()
	defer mu.Unlock()
	if writes != 1 {
		t.Fatal("duplicate split expense", writes)
	}
}

func TestPantryCreateReplayPrecedesBarcodeUniquenessCheck(t *testing.T) {
	lk := newLinkKitchen(t, nil)
	call := func(body string) (int, map[string]any) {
		req, _ := http.NewRequest("POST", lk.url+"/api/v1/pantry", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+lk.aliceTok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "pantry-create")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	body := `{"name":"Milk","quantity":2,"barcode":"123456","local":true}`
	code, first := call(body)
	if code != 201 {
		t.Fatal(code, first)
	}
	code, replay := call(body)
	if code != 201 || replay["id"] != first["id"] {
		t.Fatal(code, replay)
	}
	code, _ = call(`{"name":"Milk","quantity":3,"barcode":"123456","local":true}`)
	if code != 409 {
		t.Fatal("conflict", code)
	}
	if _, err := lk.st.DB.Exec(`DELETE FROM pantry WHERE id=?`, int64(first["id"].(float64))); err != nil {
		t.Fatal(err)
	}
	replacement := lk.ok(lk.aliceTok, "POST", "/api/v1/pantry", map[string]any{"name": "New milk", "local": true})
	if replacement["id"].(float64) <= first["id"].(float64) {
		t.Fatal("deleted pantry ID reused")
	}
	if code, _ := call(body); code != 404 {
		t.Fatal("receipt resolved to replacement pantry item", code)
	}

}
