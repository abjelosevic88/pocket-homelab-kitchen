package server

import (
	"fmt"
	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/units"
)

func (s *Server) handleListPantry(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, err := s.store.ListPantry(r.Context(), actorID(r.Context()), store.PantryFilter{Query: q.Get("q"), Location: q.Get("location"), Category: q.Get("category"), Low: queryBool(r, "low"), Expiring: queryBool(r, "expiring"), Archived: queryBool(r, "archived")})
	if err != nil {
		s.fail(w, err, "pantry")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handlePantryFacets(w http.ResponseWriter, r *http.Request) {
	locs, cats, err := s.store.PantryFacets(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "facets")
		return
	}
	if locs == nil {
		locs = []string{}
	}
	if cats == nil {
		cats = []string{}
	}
	synced, _, _ := s.store.CacheGet(r.Context(), syncedKey(actorID(r.Context())))
	total, low, expiring := s.store.PantryStats(r.Context(), actorID(r.Context()))
	writeJSON(w, http.StatusOK, map[string]any{"locations": locs, "categories": cats, "synced_at": synced, "total": total, "low": low, "expiring": expiring})
}

func (s *Server) handleGetPantry(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPantry(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get pantry item")
		return
	}
	// recipes that use it
	recipes, _ := s.store.ListRecipes(r.Context(), actorID(r.Context()), store.RecipeFilter{Query: p.Name, Limit: 10})
	writeJSON(w, http.StatusOK, map[string]any{"item": p, "recipes": recipes})
}

type pantryInput struct {
	Name        *string  `json:"name"`
	Quantity    *float64 `json:"quantity"`
	Unit        *string  `json:"unit"`
	MinQuantity *float64 `json:"min_quantity"`
	Location    *string  `json:"location"`
	Category    *string  `json:"category"`
	Barcode     *string  `json:"barcode"`
	Brand       *string  `json:"brand"`
	ExpiryDate  *string  `json:"expiry_date"`
	Notes       *string  `json:"notes"`
	PhotoURL    *string  `json:"photo_url"`
	Archived    *bool    `json:"archived"`
	Local       bool     `json:"local"` // create here only, do not mirror into Inventory
}

func applyPantry(p *store.PantryItem, in pantryInput) string {
	if in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if p.Name == "" {
		return "a name is required"
	}
	if in.Quantity != nil {
		p.Quantity = max(0, *in.Quantity)
	}
	if in.Unit != nil {
		p.Unit = units.Normalize(*in.Unit)
	}
	if in.MinQuantity != nil {
		p.MinQuantity = max(0, *in.MinQuantity)
	}
	if in.Location != nil {
		p.Location = strings.TrimSpace(*in.Location)
	}
	if in.Category != nil {
		p.Category = strings.TrimSpace(*in.Category)
	}
	if in.Barcode != nil {
		p.Barcode = strings.TrimSpace(*in.Barcode)
	}
	if in.Brand != nil {
		p.Brand = strings.TrimSpace(*in.Brand)
	}
	if in.ExpiryDate != nil {
		if !validDate(*in.ExpiryDate) {
			return "expiry_date must be YYYY-MM-DD"
		}
		p.ExpiryDate = *in.ExpiryDate
	}
	if in.Notes != nil {
		p.Notes = strings.TrimSpace(*in.Notes)
	}
	if in.PhotoURL != nil {
		p.PhotoURL = strings.TrimSpace(*in.PhotoURL)
	}
	if in.Archived != nil {
		p.Archived = *in.Archived
	}
	return ""
}

// createPantry stores a row and, when Pocket Inventory is connected, creates
// the inventory item carrying the pantry tag and mirrors it.
func (s *Server) createPantry(r *http.Request, cl *clients, p *store.PantryItem, op ...*core.Operation) error {
	inv, err := cl.inventory(r.Context(), scopeItemsWrite)
	if err != nil {
		return err
	}
	if p.Location == "" {
		p.Location = cl.st.InventoryLocation
	}
	return s.submitPantry(r, cl, p, pantryWork{Create: p, Mirror: inv != nil, Tags: cl.st.InventoryTag}, op...)
}

func (s *Server) handleCreatePantry(w http.ResponseWriter, r *http.Request) {
	var in pantryInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	op, err := core.NewOperation(fmt.Sprintf("%d:pantry:create", actorID(r.Context())), r.Header.Get("Idempotency-Key"), in)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if s.replayPantry(w, r, op, http.StatusCreated) {
		return
	}
	p := &store.PantryItem{Quantity: 1}
	if msg := applyPantry(p, in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	cl := s.clients(r.Context(), actorID(r.Context()))
	cl.noInventory = in.Local
	if p.Barcode != "" {
		if ex, err := s.store.GetPantryByBarcode(r.Context(), actorID(r.Context()), p.Barcode); err == nil && !ex.Archived {
			if s.replayPantry(w, r, op, http.StatusCreated) {
				return
			}
			writeErr(w, http.StatusConflict, "that barcode is already in the pantry as "+ex.Name)
			return
		}
	}
	if err := s.createPantry(r, cl, p, op); err != nil {
		s.pantryFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleUpdatePantry(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPantry(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get pantry item")
		return
	}
	var in pantryInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	before := *p
	if msg := applyPantry(p, in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	cl := s.clients(r.Context(), actorID(r.Context()))
	var inv *siblingConn
	if p.InventoryID != nil {
		if inv, err = cl.inventory(r.Context(), scopeItemsWrite); err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
	}
	if inv != nil {
		patch := map[string]any{"name": p.Name, "unit": p.Unit, "min_quantity": p.MinQuantity, "barcode": p.Barcode, "brand": p.Brand, "expiry_date": p.ExpiryDate, "notes": p.Notes}
		if p.Location != before.Location {
			patch["location"] = p.Location
		}
		if p.Category != before.Category {
			patch["category"] = p.Category
		}
		if p.Quantity != before.Quantity {
			patch["quantity"] = p.Quantity
			patch["note"] = "Set in Pocket Kitchen"
		}
		if it, err := inv.client.UpdateItem(r.Context(), *p.InventoryID, patch); err != nil {
			writeErr(w, http.StatusBadGateway, "Pocket Inventory: "+err.Error())
			return
		} else {
			p.Quantity, p.Location, p.Category = it.Quantity, it.Location, it.Category
			p.SyncedAt = store.Now()
		}
		if in.Archived != nil && *in.Archived && !before.Archived {
			_ = inv.client.ArchiveItem(r.Context(), *p.InventoryID)
		}
	}
	if err := s.store.UpdatePantry(r.Context(), actorID(r.Context()), p); err != nil {
		s.fail(w, err, "update pantry item")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleDeletePantry removes the row; with ?inventory=archive the mirrored
// inventory item is archived too (default: it only loses the pantry tag).
func (s *Server) handleDeletePantry(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPantry(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get pantry item")
		return
	}
	cl := s.clients(r.Context(), actorID(r.Context()))
	if p.InventoryID != nil {
		if r.URL.Query().Get("inventory") == "archive" {
			if inv, _ := cl.inventory(r.Context(), scopeItemsWrite); inv != nil {
				_ = inv.client.ArchiveItem(r.Context(), *p.InventoryID)
			}
		} else if inv, _ := cl.inventory(r.Context(), scopeItemsRead, scopeItemsWrite); inv != nil {
			if it, err := inv.client.GetItem(r.Context(), *p.InventoryID); err == nil {
				_, _ = inv.client.UpdateItem(r.Context(), *p.InventoryID, map[string]any{"tags": removeTag(it.Tags, cl.st.InventoryTag)})
			}
		}
	}
	if err := s.store.DeletePantry(r.Context(), actorID(r.Context()), p.ID); err != nil {
		s.fail(w, err, "delete pantry item")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func removeTag(tags, tag string) string {
	var out []string
	for _, t := range strings.Split(tags, ",") {
		if t = strings.TrimSpace(t); t != "" && !strings.EqualFold(t, tag) {
			out = append(out, t)
		}
	}
	return strings.Join(out, ", ")
}

func (s *Server) handleAdjustPantry(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPantry(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get pantry item")
		return
	}
	var in struct {
		Delta    *float64 `json:"delta"`
		Quantity *float64 `json:"quantity"`
		Note     string   `json:"note"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	op, err := core.NewOperation(fmt.Sprintf("%d:pantry:adjust:%d", actorID(r.Context()), p.ID), r.Header.Get("Idempotency-Key"), in)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if s.replayPantry(w, r, op, http.StatusOK) {
		return
	}
	cl := s.clients(r.Context(), actorID(r.Context()))
	note := strings.TrimSpace(in.Note)
	if note == "" {
		note = "Pocket Kitchen"
	}
	step := pantryWork{PantryID: p.ID, InventoryID: p.InventoryID, Note: note}
	if in.Quantity != nil {
		q := max(0, *in.Quantity)
		step.Quantity = &q
	} else if in.Delta != nil {
		step.Delta = *in.Delta
	} else {
		writeErr(w, 400, "delta or quantity is required")
		return
	}
	if err := s.submitPantry(r, cl, p, step, op); err != nil {
		s.pantryFailure(w, err)
		return
	}

	writeJSON(w, http.StatusOK, p)
}
