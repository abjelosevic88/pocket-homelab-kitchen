package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/foodfacts"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/units"
)

// syncPantry pulls every Pocket Inventory item carrying the pantry tag into
// the mirror table: new ones are created, known ones updated, and mirrors
// whose item lost the tag (or was archived) are archived here. It runs for one
// user with that user's own Inventory connection into that user's pantry.
func (s *Server) syncPantry(ctx context.Context, uid int64) (map[string]int, error) {
	s.syncMu.Lock()
	if s.syncing[uid] {
		s.syncMu.Unlock()
		return nil, errors.New("a sync is already running")
	}
	s.syncing[uid] = true
	s.syncMu.Unlock()
	defer func() { s.syncMu.Lock(); delete(s.syncing, uid); s.syncMu.Unlock() }()

	cl := s.clients(ctx, uid)
	if !cl.inventory.Configured() {
		return nil, errors.New("Pocket Inventory is not configured")
	}
	items, err := cl.inventory.ItemsByTag(ctx, cl.st.InventoryTag)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{"created": 0, "updated": 0, "archived": 0, "total": 0}
	keep := make([]int64, 0, len(items))
	for _, it := range items {
		if it.Archived {
			continue
		}
		keep = append(keep, it.ID)
		counts["total"]++
		p, err := s.store.GetPantryByInventory(ctx, uid, it.ID)
		if err != nil {
			// a local row with the same name adopts the inventory item
			if lp, err2 := s.store.FindPantryByName(ctx, uid, it.Name); err2 == nil && lp.InventoryID == nil {
				p = lp
			} else {
				id := it.ID
				p = &store.PantryItem{InventoryID: &id}
				fillPantry(p, it, cl.st)
				if err := s.store.CreatePantry(ctx, uid, p); err != nil {
					s.log.Warn("pantry sync: create", "item", it.Name, "err", err)
					continue
				}
				counts["created"]++
				continue
			}
		}
		id := it.ID
		p.InventoryID = &id
		fillPantry(p, it, cl.st)
		if err := s.store.UpdatePantry(ctx, uid, p); err != nil {
			s.log.Warn("pantry sync: update", "item", it.Name, "err", err)
			continue
		}
		counts["updated"]++
	}
	n, _ := s.store.ArchiveMirrorsNotIn(ctx, uid, keep)
	counts["archived"] = int(n)
	_ = s.store.CachePut(ctx, syncedKey(uid), store.Now())
	return counts, nil
}

// syncedKey is the cache key remembering a user's last pantry sync.
func syncedKey(uid int64) string { return "pantry:synced_at:" + itoa64(uid) }

func fillPantry(p *store.PantryItem, it pocket.Item, st store.Settings) {
	p.Name, p.Quantity, p.Unit, p.MinQuantity = it.Name, it.Quantity, units.Normalize(it.Unit), it.MinQuantity
	p.Location, p.Category, p.Barcode, p.Brand, p.ExpiryDate, p.Notes = it.Location, it.Category, it.Barcode, it.Brand, it.ExpiryDate, it.Notes
	if it.Photo != "" {
		p.PhotoURL = strings.TrimRight(st.InventoryURL, "/") + "/api/v1/items/" + itoa64(it.ID) + "/photo"
	}
	p.Archived = false
	p.SyncedAt = store.Now()
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func (s *Server) handleSyncPantry(w http.ResponseWriter, r *http.Request) {
	counts, err := s.syncPantry(r.Context(), actorID(r.Context()))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "counts": counts})
}

// lookupResult is what a barcode resolves to, from the nearest source.
type lookupResult struct {
	Barcode   string             `json:"barcode"`
	Source    string             `json:"source"` // pantry | inventory | openfoodfacts | none
	Pantry    *store.PantryItem  `json:"pantry,omitempty"`
	Inventory *pocket.Item       `json:"inventory,omitempty"`
	Product   *foodfacts.Product `json:"product,omitempty"`
	Error     string             `json:"error,omitempty"`
}

// handleLookup resolves a barcode: the pantry first, then Pocket Inventory
// (an item that is not yet tagged as pantry), then Open Food Facts.
func (s *Server) handleLookup(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("barcode"))
	if code == "" {
		writeErr(w, http.StatusBadRequest, "barcode is required")
		return
	}
	ctx := r.Context()
	out := lookupResult{Barcode: code, Source: "none"}
	if p, err := s.store.GetPantryByBarcode(ctx, actorID(ctx), code); err == nil {
		out.Source, out.Pantry = "pantry", p
		writeJSON(w, http.StatusOK, out)
		return
	}
	cl := s.clients(ctx, actorID(ctx))
	if cl.inventory.Configured() {
		if it, err := cl.inventory.LookupBarcode(ctx, code); err != nil {
			out.Error = "Pocket Inventory: " + err.Error()
		} else if it != nil {
			out.Source, out.Inventory = "inventory", it
			writeJSON(w, http.StatusOK, out)
			return
		}
	}
	if cl.st.FoodFacts {
		if v, _, ok := s.store.CacheGet(ctx, "off:"+code); ok {
			var p foodfacts.Product
			if jsonUnmarshal(v, &p) == nil && p.Name != "" {
				out.Source, out.Product = "openfoodfacts", &p
				writeJSON(w, http.StatusOK, out)
				return
			}
		}
		p, err := s.off.Lookup(ctx, code)
		if err != nil {
			if !errors.Is(err, foodfacts.ErrNotFound) {
				out.Error = err.Error()
			}
		} else {
			out.Source, out.Product = "openfoodfacts", p
			_ = s.store.CachePut(ctx, "off:"+code, jsonMarshal(p))
		}
	}
	writeJSON(w, http.StatusOK, out)
}
