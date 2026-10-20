package server

import (
	"database/sql"
	"errors"
	"fmt"
	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
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
	s.workflowMu.Lock()
	defer s.workflowMu.Unlock()
	operation, err := core.NewOperation(fmt.Sprintf("%d:cook:%d", actorID(ctx), rc.ID), r.Header.Get("Idempotency-Key"), in)
	if err != nil {
		return nil, err
	}
	if id, found, err := s.replayWorkflow(ctx, operation); err != nil {
		return nil, err
	} else if found {
		var wf kitchenWorkflow
		if err := s.loadWorkflow(ctx, id, &wf); err != nil {
			return nil, err
		}
		if wf.Cook == nil {
			return nil, fmt.Errorf("invalid cook receipt")
		}
		s.drainActions(ctx, actorID(ctx))
		if !s.workflowResult(ctx, id, wf.Cook) {
			wf.Cook.Warnings = append(wf.Cook.Warnings, "Cook saved; integration updates are pending and will retry automatically.")
		}
		return wf.Cook, nil
	}

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
	res := &cookResult{Cook: *c, Recipe: *rc, Deducted: []string{}, NotInPantry: []string{}, Warnings: []string{}}
	cl := s.clients(ctx, uid)
	wf := kitchenWorkflow{Cook: res}
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
				p, err = s.store.GetPantry(ctx, uid, ing.PantryID)
			}
			if p == nil || err != nil {
				p, err = s.store.FindPantryByName(ctx, uid, ing.Name)
			}
			if err != nil || p == nil || p.Archived {
				if !ing.Optional {
					res.NotInPantry = append(res.NotInPantry, ing.Name)
				}
				continue
			}
			need, ok := units.Convert(ing.Qty*factor, ing.Unit, p.Unit)
			if !ok {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s cannot convert to %s", ing.Name, ing.Unit, p.Unit))
				continue
			}
			wf.Steps = append(wf.Steps, pantryWork{PantryID: p.ID, InventoryID: p.InventoryID, Delta: -need, Note: "Cooked " + rc.Title})
		}
	}
	if habit {
		wf.HabitID = cl.st.HabitID
	}
	var actionID string
	if err := s.store.CreateCook(ctx, uid, c, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE recipes SET times_cooked=times_cooked+1,last_cooked=CASE WHEN last_cooked<? THEN ? ELSE last_cooked END,rating=CASE WHEN ?>0 THEN ? ELSE rating END,updated_at=? WHERE id=? AND user_id=?`, in.Date, in.Date, in.Rating, in.Rating, store.Now(), rc.ID, uid); err != nil {
			return err
		}
		if c.PlanID != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE plan SET cooked_at=?,cook_id=? WHERE id=? AND user_id=?`, store.Now(), c.ID, *c.PlanID, uid); err != nil {
				return err
			}
		}
		res.Cook = *c
		var err error
		actionID, err = core.QueueAction(ctx, tx, uid, "kitchen.workflow", wf)
		if err != nil {
			return err
		}
		return operation.Save(ctx, tx, actionID)
	}); err != nil {
		return nil, err
	}
	s.drainActions(ctx, uid)
	if !s.workflowResult(ctx, actionID, res) {
		res.Warnings = append(res.Warnings, "Cook saved; integration updates are pending and will retry automatically.")
	}
	return res, nil
}

func orPcs(u string) string {
	if u == "" {
		return "pcs"
	}
	return u
}
