package server

import (
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
)

func (s *Server) handleListPlan(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.GetSettings(r.Context(), actorID(r.Context()))
	from, to := weekRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"), st.WeekStart)
	if !validDate(from) || !validDate(to) {
		writeErr(w, http.StatusBadRequest, "from/to must be YYYY-MM-DD")
		return
	}
	entries, err := s.store.ListPlan(r.Context(), actorID(r.Context()), from, to)
	if err != nil {
		s.fail(w, err, "list plan")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "entries": entries})
}

type planInput struct {
	Date     *string `json:"date"`
	Slot     *string `json:"slot"`
	RecipeID *int64  `json:"recipe_id"`
	Title    *string `json:"title"`
	Servings *int    `json:"servings"`
	Notes    *string `json:"notes"`
	Cooked   *bool   `json:"cooked"`
}

func (s *Server) applyPlan(r *http.Request, p *store.PlanEntry, in planInput) string {
	if in.Date != nil {
		p.Date = *in.Date
	}
	if !validDate(p.Date) || p.Date == "" {
		return "date must be YYYY-MM-DD"
	}
	if in.Slot != nil {
		p.Slot = strings.ToLower(*in.Slot)
	}
	if p.Slot == "" {
		p.Slot = "dinner"
	}
	if !store.ValidSlot(p.Slot) {
		return "unknown slot " + p.Slot
	}
	if in.RecipeID != nil {
		if *in.RecipeID > 0 {
			if _, err := s.store.GetRecipe(r.Context(), actorID(r.Context()), *in.RecipeID); err != nil {
				return "recipe not found"
			}
			p.RecipeID = in.RecipeID
		} else {
			p.RecipeID = nil
		}
	}
	if in.Title != nil {
		p.Title = strings.TrimSpace(*in.Title)
	}
	if p.RecipeID == nil && p.Title == "" {
		return "pick a recipe or type what you'll eat"
	}
	if in.Servings != nil {
		p.Servings = max(0, *in.Servings)
	}
	if in.Notes != nil {
		p.Notes = strings.TrimSpace(*in.Notes)
	}
	if in.Cooked != nil {
		if *in.Cooked && p.CookedAt == "" {
			p.CookedAt = store.Now()
		} else if !*in.Cooked {
			p.CookedAt = ""
			p.CookID = nil
		}
	}
	return ""
}

func (s *Server) handleCreatePlan(w http.ResponseWriter, r *http.Request) {
	var in planInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := &store.PlanEntry{Date: store.Today(), Slot: "dinner"}
	uid := actorID(r.Context())
	if uid > 0 {
		p.CreatedBy = &uid
	}
	if msg := s.applyPlan(r, p, in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.CreatePlan(r.Context(), actorID(r.Context()), p); err != nil {
		s.fail(w, err, "create plan entry")
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleUpdatePlan(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPlan(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get plan entry")
		return
	}
	var in planInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := s.applyPlan(r, p, in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdatePlan(r.Context(), actorID(r.Context()), p); err != nil {
		s.fail(w, err, "update plan entry")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePlan(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeletePlan(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete plan entry")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCookPlan(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPlan(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get plan entry")
		return
	}
	var in cookInput
	_ = decode(r, &in)
	in.PlanID = p.ID
	if in.Servings <= 0 {
		in.Servings = p.Servings
	}
	if in.Date == "" {
		in.Date = p.Date
		if in.Date > store.Today() {
			in.Date = store.Today()
		}
	}
	if p.RecipeID == nil {
		// free-text entry: just mark it cooked and the habit
		p.CookedAt = store.Now()
		c := &store.Cook{Date: in.Date, Servings: in.Servings, Notes: p.Title}
		c.PlanID = &p.ID
		if uid := actorID(r.Context()); uid > 0 {
			c.UserID = &uid
		}
		_ = s.store.CreateCook(r.Context(), actorID(r.Context()), c)
		p.CookID = &c.ID
		_ = s.store.UpdatePlan(r.Context(), actorID(r.Context()), p)
		cl := s.clients(r.Context(), actorID(r.Context()))
		marked := false
		if (in.MarkHabit == nil || *in.MarkHabit) && cl.habits.Configured() && cl.st.HabitID > 0 {
			if err := cl.habits.MarkHabit(r.Context(), cl.st.HabitID, in.Date, 1); err == nil {
				marked = true
				_ = s.store.UpdateCookFlags(r.Context(), actorID(r.Context()), c.ID, true, 0)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"cook": c, "habit_marked": marked, "deducted": []string{}, "not_in_pantry": []string{}, "warnings": []string{}})
		return
	}
	rc, err := s.store.GetRecipe(r.Context(), actorID(r.Context()), *p.RecipeID)
	if err != nil {
		s.fail(w, err, "get recipe")
		return
	}
	res, err := s.cook(r, rc, in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handlePlanToList turns the uncooked entries of a date range into shopping lines.
func (s *Server) handlePlanToList(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From       string `json:"from"`
		To         string `json:"to"`
		ListID     int64  `json:"list_id"`
		SkipPantry bool   `json:"skip_pantry"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(r.Context(), actorID(r.Context()))
	from, to := weekRange(in.From, in.To, st.WeekStart)
	entries, err := s.store.ListPlan(r.Context(), actorID(r.Context()), from, to)
	if err != nil {
		s.fail(w, err, "list plan")
		return
	}
	list, err := s.resolveList(r, in.ListID)
	if err != nil {
		s.fail(w, err, "list")
		return
	}
	total, skippedAll := 0, []string{}
	recipes := 0
	for _, p := range entries {
		if p.RecipeID == nil || p.CookedAt != "" {
			continue
		}
		rc, err := s.store.GetRecipe(r.Context(), actorID(r.Context()), *p.RecipeID)
		if err != nil {
			continue
		}
		recipes++
		added, skipped := s.addRecipeToList(r, list.ID, rc, p.Servings, in.SkipPantry, p.ID)
		total += len(added)
		skippedAll = append(skippedAll, skipped...)
	}
	l, _ := s.store.GetList(r.Context(), actorID(r.Context()), list.ID)
	writeJSON(w, http.StatusOK, map[string]any{"list": l, "recipes": recipes, "added": total, "skipped": skippedAll, "from": from, "to": to})
}

// handleCopyPlan copies a date range forward by N days (e.g. last week → this week).
func (s *Server) handleCopyPlan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
		Days int    `json:"days"`
	}
	if err := decode(r, &in); err != nil || !validDate(in.From) || !validDate(in.To) || in.From == "" || in.To == "" || in.Days == 0 {
		writeErr(w, http.StatusBadRequest, "from, to and days are required")
		return
	}
	entries, err := s.store.ListPlan(r.Context(), actorID(r.Context()), in.From, in.To)
	if err != nil {
		s.fail(w, err, "list plan")
		return
	}
	uid := actorID(r.Context())
	n := 0
	for _, p := range entries {
		np := store.PlanEntry{Date: store.AddDays(p.Date, in.Days), Slot: p.Slot, RecipeID: p.RecipeID, Title: p.Title, Servings: p.Servings, Notes: p.Notes}
		if uid > 0 {
			np.CreatedBy = &uid
		}
		if err := s.store.CreatePlan(r.Context(), actorID(r.Context()), &np); err == nil {
			n++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"copied": n})
}
