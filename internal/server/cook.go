package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/units"
)

// cookResult is what "cook it" did.
type cookResult struct {
	Cook        store.Cook   `json:"cook"`
	Recipe      store.Recipe `json:"recipe"`
	Deducted    []string     `json:"deducted"` // "200 g flour (1.2 kg left)"
	NotInPantry []string     `json:"not_in_pantry"`
	HabitMarked bool         `json:"habit_marked"`
	Warnings    []string     `json:"warnings"`
}

// cook records a cook, deducts ingredients from the pantry (through Pocket
// Inventory when the row mirrors an item) and marks the "cooked at home" habit.
func (s *Server) cook(r *http.Request, rc *store.Recipe, in cookInput) (*cookResult, error) {
	ctx := r.Context()
	if in.Date == "" {
		in.Date = store.Today()
	}
	if !validDate(in.Date) || in.Date > store.Today() {
		return nil, errors.New("date must be YYYY-MM-DD and not in the future")
	}
	if in.Servings <= 0 {
		in.Servings = rc.Servings
	}
	deduct := in.DeductPantry == nil || *in.DeductPantry
	habit := in.MarkHabit == nil || *in.MarkHabit
	uid := actorID(ctx)
	c := &store.Cook{Date: in.Date, Servings: in.Servings, Notes: strings.TrimSpace(in.Notes), Rating: min(5, max(0, in.Rating))}
	c.RecipeID = &rc.ID
	if uid > 0 {
		c.UserID = &uid
	}
	if in.PlanID > 0 {
		if _, err := s.store.GetPlan(ctx, uid, in.PlanID); err != nil {
			in.PlanID = 0 // not one of the caller's plan entries
		} else {
			c.PlanID = &in.PlanID
		}
	}
	if err := s.store.CreateCook(ctx, actorID(ctx), c); err != nil {
		return nil, err
	}
	_ = s.store.MarkCooked(ctx, actorID(ctx), rc.ID, in.Date)
	if in.Rating > 0 {
		rc.Rating = in.Rating
		_ = s.store.UpdateRecipe(ctx, actorID(ctx), rc)
	}
	if in.PlanID > 0 {
		if p, err := s.store.GetPlan(ctx, actorID(ctx), in.PlanID); err == nil {
			p.CookedAt = store.Now()
			p.CookID = &c.ID
			_ = s.store.UpdatePlan(ctx, actorID(ctx), p)
		}
	}
	res := &cookResult{Cook: *c, Deducted: []string{}, NotInPantry: []string{}, Warnings: []string{}}
	cl := s.clients(ctx, actorID(ctx))
	factor := 1.0
	if rc.Servings > 0 {
		factor = float64(in.Servings) / float64(rc.Servings)
	}
	if deduct {
		for _, ing := range rc.Ingredients {
			if ing.Qty <= 0 {
				continue
			}
			var p *store.PantryItem
			var err error
			if ing.PantryID > 0 {
				p, err = s.store.GetPantry(ctx, actorID(ctx), ing.PantryID)
			}
			if p == nil || err != nil {
				p, err = s.store.FindPantryByName(ctx, actorID(ctx), ing.Name)
			}
			if err != nil || p == nil || p.Archived {
				if !ing.Optional {
					res.NotInPantry = append(res.NotInPantry, ing.Name)
				}
				continue
			}
			need, ok := units.Convert(ing.Qty*factor, ing.Unit, p.Unit)
			if !ok {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s is not convertible to the pantry's %s", ing.Name, units.Format(ing.Qty*factor, ing.Unit), orPcs(p.Unit)))
				continue
			}
			delta := -need
			if p.Quantity+delta < 0 {
				delta = -p.Quantity
			}
			if delta == 0 {
				res.Warnings = append(res.Warnings, p.Name+" was already at 0")
				continue
			}
			if err := s.adjustPantry(r, cl, p, delta, "Cooked "+rc.Title); err != nil {
				res.Warnings = append(res.Warnings, p.Name+": "+err.Error())
				continue
			}
			res.Deducted = append(res.Deducted, fmt.Sprintf("%s %s (%s left)", units.Format(-delta, p.Unit), p.Name, units.FormatZero(p.Quantity, p.Unit)))
		}
	}
	if habit && cl.st.HabitID > 0 {
		if hb, err := cl.habits(ctx, scopeEntriesWrite); err != nil {
			if !isNotConnected(err) {
				res.Warnings = append(res.Warnings, err.Error())
			}
		} else if err := hb.client.MarkHabit(ctx, cl.st.HabitID, in.Date, 1); err != nil {
			res.Warnings = append(res.Warnings, "Pocket Habits: "+err.Error())
		} else {
			res.HabitMarked = true
		}
	}
	_ = s.store.UpdateCookFlags(ctx, actorID(ctx), c.ID, res.HabitMarked, len(res.Deducted))
	c.HabitMarked, c.PantryDeducted = res.HabitMarked, len(res.Deducted)
	res.Cook = *c
	if nr, err := s.store.GetRecipe(ctx, actorID(ctx), rc.ID); err == nil {
		res.Recipe = *nr
	}
	s.publish("kitchen.cooked", fmt.Sprintf("recipe:%d", rc.ID), map[string]any{
		"user_email": userFrom(ctx).Email, "recipe_id": rc.ID, "recipe": rc.Title, "date": in.Date, "servings": in.Servings, "rating": in.Rating,
		"deducted": res.Deducted, "habit_marked": res.HabitMarked, "cook_id": c.ID,
	})
	return res, nil
}

func orPcs(u string) string {
	if u == "" {
		return "pcs"
	}
	return u
}

// adjustPantry changes a pantry quantity, through Pocket Inventory when the
// row mirrors an item; p is updated in place with the new quantity.
func (s *Server) adjustPantry(r *http.Request, cl *clients, p *store.PantryItem, delta float64, note string) error {
	ctx := r.Context()
	var inv *siblingConn
	if p.InventoryID != nil {
		var err error
		if inv, err = cl.inventory(ctx, scopeItemsAdjust); err != nil {
			return err
		}
	}
	if inv != nil {
		it, err := inv.client.AdjustItem(ctx, *p.InventoryID, delta, note)
		if err != nil {
			return err
		}
		p.Quantity = it.Quantity
		p.SyncedAt = store.Now()
	} else {
		p.Quantity += delta
		if p.Quantity < 0 {
			p.Quantity = 0
		}
	}
	return s.store.UpdatePantry(ctx, actorID(ctx), p)
}
