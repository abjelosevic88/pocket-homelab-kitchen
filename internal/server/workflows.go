package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/units"
	"net/http"
	"strings"
	"time"
)

// A workflow freezes the intended writes before any sibling is called. Each
// step has a stable key and a local receipt, so a restart resumes safely.
type pantryWork struct {
	PantryID    int64
	InventoryID *int64
	Delta       float64
	Quantity    *float64
	Note        string
	Create      *store.PantryItem
	Mirror      bool
	Tags        string
}
type kitchenWorkflow struct {
	PantryOnly bool
	Cook       *cookResult
	Checkout   *checkoutResult
	Steps      []pantryWork
	HabitID    int64
	Group      *pocket.Group
	Expense    *pocket.ExpenseInput
	SplitURL   string
}

func (s *Server) actionsLoop(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		if ctx.Err() != nil || s.store.DB.PingContext(ctx) != nil {
			return
		}
		s.drainActions(ctx, 0)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (s *Server) drainActions(ctx context.Context, uid int64) {
	s.actionMu.Lock()
	defer s.actionMu.Unlock()
	for n := 0; n < 100; n++ {
		actions, err := core.PendingActions(ctx, s.store.DB, uid)
		if err != nil {
			s.log.Warn("read pending actions", "err", err)
			return
		}
		if len(actions) == 0 {
			return
		}
		for _, a := range actions {
			if err := s.runWorkflow(ctx, a); err != nil {
				_ = core.RetryAction(ctx, s.store.DB, a, err)
				s.log.Warn("integration action waiting to retry", "kind", a.Kind, "err", err)
			}
		}
	}
}
func (s *Server) workflowResult(ctx context.Context, id string, out any) bool {
	var raw []byte
	var done int64
	if err := s.store.DB.QueryRowContext(ctx, `SELECT done_at,response FROM pocket_actions WHERE id=?`, id).Scan(&done, &raw); err != nil || done == 0 {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}
func (s *Server) runWorkflow(ctx context.Context, a core.Action) error {
	u, err := s.store.GetUser(ctx, a.UserID)
	if err != nil {
		return err
	}
	var wf kitchenWorkflow
	if err := json.Unmarshal(a.Payload, &wf); err != nil {
		return err
	}
	if a.Kind != "kitchen.workflow" {
		return fmt.Errorf("unknown action %s", a.Kind)
	}
	cl := s.clients(ctx, a.UserID)
	var lastPantry *store.PantryItem
	for i, step := range wf.Steps {
		p, err := s.pantryStep(ctx, a.UserID, fmt.Sprintf("%s:pantry:%d", a.ID, i), cl, step)
		if err != nil {
			if isNotConnected(err) && !wf.PantryOnly {
				if wf.Cook != nil {
					wf.Cook.Warnings = append(wf.Cook.Warnings, err.Error())
				}
				if wf.Checkout != nil {
					wf.Checkout.Warnings = append(wf.Checkout.Warnings, err.Error())
				}
				continue
			}
			return err
		}
		lastPantry = p
		if wf.Cook != nil {
			wf.Cook.Deducted = append(wf.Cook.Deducted, fmt.Sprintf("%s %s (%s left)", units.Format(-step.Delta, p.Unit), p.Name, units.FormatZero(p.Quantity, p.Unit)))
		}
		if wf.Checkout != nil {
			wf.Checkout.Restocked = append(wf.Checkout.Restocked, fmt.Sprintf("%s (%s)", p.Name, units.FormatZero(p.Quantity, p.Unit)))
		}
	}
	if wf.Cook != nil && wf.HabitID > 0 {
		hb, err := cl.habits(ctx, scopeEntriesWrite)
		if err != nil {
			if !isNotConnected(err) {
				return err
			}
		} else {
			if err := hb.client.MarkHabit(ctx, wf.HabitID, wf.Cook.Cook.Date, 1); err != nil {
				return err
			}
			wf.Cook.HabitMarked = true
		}
	}

	if wf.Checkout != nil && wf.Group != nil && wf.Expense != nil {
		sp, err := cl.split(ctx, scopeGroupsRead, scopeExpensesWrite)
		if err != nil {
			return err
		}
		e, err := sp.client.CreateExpense(ctx, wf.Group, *wf.Expense, a.ID+":split")
		if err != nil {
			return err
		}
		wf.Checkout.Purchase.SplitGroupID, wf.Checkout.Purchase.SplitExpenseID = wf.Group.ID, e.ID
		for _, m := range wf.Group.Members {
			if m.ID == wf.Expense.PaidBy {
				wf.Checkout.Purchase.PaidBy = m.Name
			}
		}
		wf.Checkout.SplitURL = wf.SplitURL
	}
	var result any
	var topic, subject string
	var payload map[string]any
	if wf.Cook != nil {
		c := &wf.Cook.Cook
		c.HabitMarked = wf.Cook.HabitMarked
		c.PantryDeducted = len(wf.Cook.Deducted)
		if c.RecipeID != nil {
			if rc, e := s.store.GetRecipe(ctx, a.UserID, *c.RecipeID); e == nil {
				wf.Cook.Recipe = *rc
			}
		}
		result = wf.Cook
		topic = "kitchen.cooked"
		subject = fmt.Sprintf("recipe:%d", wf.Cook.Recipe.ID)
		payload = map[string]any{"user_email": u.Email, "recipe_id": wf.Cook.Recipe.ID, "recipe": wf.Cook.Recipe.Title, "date": c.Date, "servings": c.Servings, "rating": c.Rating, "deducted": wf.Cook.Deducted, "habit_marked": c.HabitMarked, "cook_id": c.ID}
	} else if wf.Checkout != nil {
		p := &wf.Checkout.Purchase
		p.PantryRestocked = len(wf.Checkout.Restocked)
		if p.ListID != nil {
			wf.Checkout.List, _ = s.store.GetList(ctx, a.UserID, *p.ListID)
		}
		result = wf.Checkout
		topic = "kitchen.checkout"
		subject = fmt.Sprintf("list:%d", *p.ListID)
		payload = map[string]any{"user_email": u.Email, "list_id": *p.ListID, "list": p.ListName, "amount": p.Amount, "currency": p.Currency, "date": p.Date, "store": p.Store}
	} else if wf.PantryOnly && lastPantry != nil {
		result = lastPantry
	} else {
		return fmt.Errorf("workflow has no outcome")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return core.FinishAction(ctx, s.store.DB, a, func(tx *sql.Tx) error {
		if wf.Cook != nil {
			c := wf.Cook.Cook
			if _, err := tx.ExecContext(ctx, `UPDATE cooklog SET habit_marked=?,pantry_deducted=? WHERE id=? AND user_id=?`, c.HabitMarked, c.PantryDeducted, c.ID, a.UserID); err != nil {
				return err
			}
		}
		if wf.Checkout != nil {
			p := wf.Checkout.Purchase
			if _, err := tx.ExecContext(ctx, `UPDATE purchases SET paid_by=?,split_group_id=?,split_expense_id=?,pantry_restocked=? WHERE id=? AND user_id=?`, p.PaidBy, p.SplitGroupID, p.SplitExpenseID, p.PantryRestocked, p.ID, a.UserID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE pocket_actions SET response=? WHERE id=?`, raw, a.ID); err != nil {
			return err
		}
		if topic != "" && s.core.Configured() {
			return core.QueueEvent(ctx, tx, topic, subject, payload)
		}
		return nil
	})
}
func (s *Server) pantryStep(ctx context.Context, uid int64, key string, cl *clients, in pantryWork) (*store.PantryItem, error) {
	op, err := core.NewOperation(fmt.Sprintf("%d:pantry-step", uid), key, in)
	if err != nil {
		return nil, err
	}
	var saved store.PantryItem
	if ok, err := op.Replay(ctx, s.store.DB, &saved); err != nil {
		return nil, err
	} else if ok {
		return &saved, nil
	}
	var p *store.PantryItem
	if in.Create != nil {
		cp := *in.Create
		p = &cp
	} else {
		p, err = s.store.GetPantry(ctx, uid, in.PantryID)
		if err != nil {
			return nil, err
		}
	}
	if in.Create != nil && in.Mirror {
		conn, err := cl.inventory(ctx, scopeItemsWrite)
		if err != nil {
			return nil, err
		}
		if conn == nil {
			return nil, &notConnected{msg: "Pocket Inventory is switched off"}
		}
		q, m := p.Quantity, p.MinQuantity
		it, err := conn.client.CreateItem(ctx, pocket.ItemInput{Name: p.Name, Quantity: &q, Unit: p.Unit, MinQuantity: &m, Barcode: p.Barcode, Brand: p.Brand, ExpiryDate: p.ExpiryDate, Notes: p.Notes, Tags: in.Tags, Location: p.Location, Category: p.Category}, key)
		if err != nil {
			return nil, err
		}
		p.InventoryID = &it.ID
		p.Quantity = it.Quantity
		p.Location, p.Category = it.Location, it.Category
		p.SyncedAt = store.Now()
	} else if in.Create == nil && in.InventoryID != nil {
		conn, err := cl.inventory(ctx, scopeItemsAdjust)
		if err != nil {
			return nil, err
		}
		if conn == nil {
			return nil, &notConnected{msg: "Pocket Inventory is switched off"}
		}
		var it *pocket.Item
		if in.Quantity != nil {
			it, err = conn.client.SetItemQuantity(ctx, *in.InventoryID, *in.Quantity, in.Note, key)
		} else {
			it, err = conn.client.AdjustItem(ctx, *in.InventoryID, in.Delta, in.Note, key)
		}
		if err != nil {
			return nil, err
		}
		p.Quantity = it.Quantity
		p.SyncedAt = store.Now()
	} else if in.Create == nil {
		if in.Quantity != nil {
			p.Quantity = max(0, *in.Quantity)
		} else {
			p.Quantity = max(0, p.Quantity+in.Delta)
		}
	}
	hook := func(tx *sql.Tx) error { return op.Save(ctx, tx, p) }
	if in.Create != nil {
		err = s.store.CreatePantry(ctx, uid, p, hook)
	} else {
		err = s.store.UpdatePantry(ctx, uid, p, hook)
	}
	return p, err
}

// prepareRestock freezes quantities, identities and creation inputs before the
// checkout commits. No sibling write happens during preparation.
func (s *Server) prepareRestock(r *http.Request, cl *clients, it store.ListItem) (*pantryWork, string) {
	ctx, uid := r.Context(), actorID(r.Context())
	var p *store.PantryItem
	var err error
	if it.PantryID != nil {
		p, err = s.store.GetPantry(ctx, uid, *it.PantryID)
	}
	if p == nil || err != nil {
		p, err = s.store.FindPantryByName(ctx, uid, it.Name)
	}
	qty := it.Qty
	if qty <= 0 {
		qty = 1
	}
	if err == nil && p != nil {
		delta, ok := units.Convert(qty, it.Unit, p.Unit)
		if !ok {
			return nil, fmt.Sprintf("%s: cannot convert %s to %s", it.Name, it.Unit, p.Unit)
		}
		return &pantryWork{PantryID: p.ID, InventoryID: p.InventoryID, Delta: delta, Note: "Bought (" + it.Name + ")"}, ""
	}
	inv, err := cl.inventory(ctx, scopeItemsWrite)
	if err != nil {
		return nil, it.Name + ": " + err.Error()
	}
	return &pantryWork{Create: &store.PantryItem{Name: it.Name, Quantity: qty, Unit: units.Normalize(it.Unit), Location: cl.st.InventoryLocation, Category: it.Aisle}, Mirror: inv != nil, Tags: cl.st.InventoryTag}, ""
}
func (s *Server) prepareSplit(ctx context.Context, cl *clients, in checkoutInput, p *store.Purchase, wf *kitchenWorkflow) error {
	if !in.Split || in.Amount <= 0 {
		return nil
	}
	sp, err := cl.split(ctx, scopeGroupsRead, scopeExpensesWrite)
	if err != nil {
		return err
	}
	if cl.st.SplitGroupID == 0 {
		return fmt.Errorf("Pocket Split: choose a group in Settings → Integrations")
	}
	g, err := sp.client.Group(ctx, cl.st.SplitGroupID)
	if err != nil {
		return err
	}
	paidBy := in.PaidBy
	if paidBy == 0 {
		paidBy = cl.st.SplitMemberID
	}
	if paidBy == 0 && len(g.Members) > 0 {
		paidBy = g.Members[0].ID
	}
	cat := in.Category
	if cat == "" {
		cat = cl.st.SplitCategory
	}
	title := "Groceries"
	if p.Store != "" {
		title += " · " + p.Store
	}
	notes := p.ItemNames
	if len(notes) > 900 {
		notes = notes[:900] + "…"
	}
	wf.Group = g
	wf.Expense = &pocket.ExpenseInput{Title: title, Amount: in.Amount, Currency: in.Currency, PaidBy: paidBy, Date: in.Date, Category: cat, Notes: notes}
	wf.SplitURL = fmt.Sprintf("%s/groups/%d", strings.TrimRight(sp.public, "/"), g.ID)
	return nil
}

func (s *Server) replayWorkflow(ctx context.Context, op *core.Operation) (string, bool, error) {
	var id string
	ok, err := op.Replay(ctx, s.store.DB, &id)
	return id, ok, err
}
func (s *Server) loadWorkflow(ctx context.Context, id string, wf *kitchenWorkflow) error {
	var raw []byte
	if err := s.store.DB.QueryRowContext(ctx, `SELECT payload FROM pocket_actions WHERE id=?`, id).Scan(&raw); err != nil {
		return err
	}
	return json.Unmarshal(raw, wf)
}

// submitPantry persists a standalone adjustment/creation before contacting Inventory.
func (s *Server) submitPantry(r *http.Request, cl *clients, p *store.PantryItem, step pantryWork, provided ...*core.Operation) error {
	ctx, uid := r.Context(), actorID(r.Context())
	s.workflowMu.Lock()
	defer s.workflowMu.Unlock()
	op, err := core.NewOperation(fmt.Sprintf("%d:pantry:%d", uid, p.ID), r.Header.Get("Idempotency-Key"), step)
	if err != nil {
		return err
	}
	if len(provided) > 0 {
		op = provided[0]
	}
	id, found, err := s.replayWorkflow(ctx, op)
	if err != nil {
		return err
	}
	if !found {
		tx, err := s.store.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		id, err = core.QueueAction(ctx, tx, uid, "kitchen.workflow", kitchenWorkflow{PantryOnly: true, Steps: []pantryWork{step}})
		if err != nil {
			return err
		}
		if err := op.Save(ctx, tx, id); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	s.drainActions(ctx, uid)
	if !s.workflowResult(ctx, id, p) {
		return fmt.Errorf("saved; inventory update is pending and will retry automatically")
	}
	return nil
}

// A completed create can be replayed before barcode validation or defaults change.
func (s *Server) replayPantry(w http.ResponseWriter, r *http.Request, op *core.Operation, status int) bool {
	id, found, err := s.replayWorkflow(r.Context(), op)
	if err != nil {
		s.pantryFailure(w, err)
		return true
	}
	if !found {
		return false
	}
	s.drainActions(r.Context(), actorID(r.Context()))
	var p store.PantryItem
	if !s.workflowResult(r.Context(), id, &p) {
		writeErr(w, 503, "saved; inventory update is pending and will retry automatically")
		return true
	}
	if _, err = s.store.GetPantry(r.Context(), actorID(r.Context()), p.ID); err != nil {
		writeErr(w, 404, "pantry item no longer exists")
		return true
	}
	writeJSON(w, status, p)
	return true
}
func (s *Server) pantryFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, core.ErrOperationConflict) {
		writeErr(w, 409, err.Error())
		return
	}
	s.fail(w, err, "pantry change")
}
