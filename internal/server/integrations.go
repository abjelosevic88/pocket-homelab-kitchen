package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/foodfacts"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/units"
)

func jsonMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// publicSettings is what one person sees: their settings with the tokens
// masked and, per Pocket app, the mode in effect (inventory_mode), whether it
// works and how (inventory_via: core | direct | "") and why not (_detail).
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	invMode, invVia, invDetail := s.connStatus(ctx, u, st, appInventory)
	splitMode, splitVia, splitDetail := s.connStatus(ctx, u, st, appSplit)
	habitsMode, habitsVia, habitsDetail := s.connStatus(ctx, u, st, appHabits)
	return map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"inventory_mode": invMode, "inventory_via": invVia, "inventory_detail": invDetail,
		"split_mode": splitMode, "split_via": splitVia, "split_detail": splitDetail,
		"habits_mode": habitsMode, "habits_via": habitsVia, "habits_detail": habitsDetail,
		"title": st.Title, "home_currency": st.HomeCurrency, "date_format": st.DateFormat, "time_format": st.TimeFormat, "week_start": st.WeekStart, "default_servings": st.DefaultServings,
		"notify_url": st.NotifyURL, "notify_format": st.NotifyFormat, "notify_hour": st.NotifyHour, "notify_plan": st.NotifyPlan, "notify_expiry": st.NotifyExpiry, "notify_configured": st.NotifyURL != "",
		"inventory_url": st.InventoryURL, "inventory_token": mask(st.InventoryToken), "inventory_tag": st.InventoryTag, "inventory_location": st.InventoryLocation, "inventory_auto_sync": st.InventoryAutoSync,
		"inventory_configured": invVia != "",
		"split_url":            st.SplitURL, "split_token": mask(st.SplitToken), "split_public_url": st.SplitPublicURL, "split_group_id": st.SplitGroupID, "split_member_id": st.SplitMemberID, "split_category": st.SplitCategory,
		"split_configured": splitVia != "" && st.SplitGroupID > 0,
		"habits_url":       st.HabitsURL, "habits_token": mask(st.HabitsToken), "habit_id": st.HabitID,
		"habits_configured": habitsVia != "" && st.HabitID > 0,
		"food_facts":        st.FoodFacts, "max_upload_bytes": s.cfg.MaxUploadBytes, "setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL,
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

// handlePutSettings saves the caller's personal settings (preferences,
// notifications, their own Inventory / Split / Habits connections). The
// box-wide keys (title, Open Food Facts) are only taken from an admin and are
// silently ignored for everyone else, since the UI always sends the whole form.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var in store.Settings
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	if keys["home_currency"] {
		if !currency.Valid(in.HomeCurrency) {
			writeErr(w, http.StatusBadRequest, "unknown currency")
			return
		}
		cur.HomeCurrency = strings.ToUpper(in.HomeCurrency)
	}
	if keys["date_format"] {
		cur.DateFormat = in.DateFormat
	}
	if keys["time_format"] {
		cur.TimeFormat = in.TimeFormat
	}
	if keys["week_start"] && (in.WeekStart == "mon" || in.WeekStart == "sun") {
		cur.WeekStart = in.WeekStart
	}
	if keys["default_servings"] && in.DefaultServings > 0 && in.DefaultServings <= 50 {
		cur.DefaultServings = in.DefaultServings
	}
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	url := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	if keys["notify_url"] {
		cur.NotifyURL = strings.TrimSpace(in.NotifyURL)
	}
	if keys["notify_format"] && in.NotifyFormat != "" {
		cur.NotifyFormat = in.NotifyFormat
	}
	if keys["notify_hour"] && in.NotifyHour >= 0 && in.NotifyHour <= 23 {
		cur.NotifyHour = in.NotifyHour
	}
	if keys["notify_plan"] {
		cur.NotifyPlan = in.NotifyPlan
	}
	if keys["notify_expiry"] {
		cur.NotifyExpiry = in.NotifyExpiry
	}
	url("inventory_url", &cur.InventoryURL, in.InventoryURL)
	secret("inventory_token", &cur.InventoryToken, in.InventoryToken)
	if keys["inventory_tag"] {
		if t := strings.ToLower(strings.TrimSpace(in.InventoryTag)); t != "" {
			cur.InventoryTag = t
		}
	}
	if keys["inventory_location"] {
		cur.InventoryLocation = strings.TrimSpace(in.InventoryLocation)
	}
	if keys["inventory_auto_sync"] {
		cur.InventoryAutoSync = in.InventoryAutoSync
	}
	url("split_url", &cur.SplitURL, in.SplitURL)
	secret("split_token", &cur.SplitToken, in.SplitToken)
	url("split_public_url", &cur.SplitPublicURL, in.SplitPublicURL)
	if keys["split_group_id"] {
		cur.SplitGroupID = max(0, in.SplitGroupID)
	}
	if keys["split_member_id"] {
		cur.SplitMemberID = max(0, in.SplitMemberID)
	}
	if keys["split_category"] {
		if c := strings.TrimSpace(in.SplitCategory); c != "" {
			cur.SplitCategory = c
		}
	}
	url("habits_url", &cur.HabitsURL, in.HabitsURL)
	secret("habits_token", &cur.HabitsToken, in.HabitsToken)
	if keys["habit_id"] {
		cur.HabitID = max(0, in.HabitID)
	}
	mode := func(k string, dst *string, v string) {
		if keys[k] && (v == modeCore || v == modeDirect || v == modeOff) {
			*dst = v
		}
	}
	mode("inventory_mode", &cur.InventoryMode, in.InventoryMode)
	mode("split_mode", &cur.SplitMode, in.SplitMode)
	mode("habits_mode", &cur.HabitsMode, in.HabitsMode)
	if keys["food_facts"] && isAdmin {
		cur.FoodFacts = in.FoodFacts
	}
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && (keys["title"] || keys["food_facts"]) {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	for _, k := range modeKeys {
		if keys[k] {
			s.reportModesSoon()
			break
		}
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(r.Context(), actorID(r.Context()))
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return in.URL
	}
	var msg string
	var err error
	switch in.Which {
	case "inventory", "split", "habits":
		var a pocketApp
		for _, x := range pocketApps {
			if x.slug == in.Which {
				a = x
			}
		}
		mode, savedURL, savedTok, _ := connSettings(st, a)
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		var conn *siblingConn
		// a token typed into the form is tried as is; otherwise the saved mode decides
		if tok := keyOr(savedTok); tok != "" && (typed || s.connMode(mode, savedTok) == modeDirect) {
			base := urlOr(savedURL)
			if base == "" {
				base = s.resolveSibling(ctx, a.slug)
			}
			conn = &siblingConn{client: pocket.New(base, tok, 15*time.Second), via: modeDirect}
		} else {
			cl := s.clients(ctx, actorID(ctx))
			// the scopes the read side of the integration needs
			read := map[string]string{"inventory": scopeItemsRead, "split": scopeGroupsRead, "habits": scopeHabitsRead}[a.slug]
			conn, err = cl.conn(ctx, a, read)
		}
		if err != nil {
			break
		}
		how := "direct connection"
		if conn.via == modeCore {
			how = "through Pocket Core"
		}
		var v string
		if v, err = conn.client.Health(ctx); err != nil {
			break
		}
		switch a.slug {
		case "inventory":
			var items []pocket.Item
			items, err = conn.client.ItemsByTag(ctx, st.InventoryTag)
			msg = fmt.Sprintf("Pocket Inventory %s, %s, %d items tagged “%s”", v, how, len(items), st.InventoryTag)
		case "split":
			var gs []pocket.Group
			gs, err = conn.client.Groups(ctx)
			msg = fmt.Sprintf("Pocket Split %s, %s, %d groups", v, how, len(gs))
		case "habits":
			var hs []pocket.Habit
			hs, err = conn.client.Habits(ctx)
			msg = fmt.Sprintf("Pocket Habits %s, %s, %d habits", v, how, len(hs))
		}
	case "notify":
		f := in.Format
		if f == "" {
			f = st.NotifyFormat
		}
		n := notify.New(urlOr(st.NotifyURL), f)
		err = n.Send(ctx, notify.Message{Title: "Pocket Kitchen", Body: "Notifications work. The daily plan and expiring pantry items will arrive here.", Priority: "info", Tags: []string{"fork_and_knife"}})
		msg = "Test message sent via " + n.Format
	case "foodfacts":
		var p *foodfacts.Product
		p, err = s.off.Lookup(ctx, "3017624010701")
		if err == nil {
			msg = "Open Food Facts reachable (" + p.Name + ")"
		}
	default:
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

// The pickers in Settings list through whichever connection is in effect;
// without one they are empty.

func (s *Server) handleSplitGroups(w http.ResponseWriter, r *http.Request) {
	cl := s.clients(r.Context(), actorID(r.Context()))
	sp, err := optional(cl.split(r.Context(), scopeGroupsRead))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if sp == nil {
		writeJSON(w, http.StatusOK, []pocket.Group{})
		return
	}
	gs, err := sp.client.Groups(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket Split: "+err.Error())
		return
	}
	// groups come without members in the list; fetch them one by one
	out := make([]pocket.Group, 0, len(gs))
	for _, g := range gs {
		if g.Archived {
			continue
		}
		if len(g.Members) == 0 {
			if full, err := sp.client.Group(r.Context(), g.ID); err == nil {
				g = *full
			}
		}
		out = append(out, g)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSplitCategories(w http.ResponseWriter, r *http.Request) {
	cl := s.clients(r.Context(), actorID(r.Context()))
	sp, err := optional(cl.split(r.Context(), scopeGroupsRead))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if sp == nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	cats, err := sp.client.Categories(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket Split: "+err.Error())
		return
	}
	if cats == nil {
		cats = []string{}
	}
	writeJSON(w, http.StatusOK, cats)
}

func (s *Server) handleHabitsList(w http.ResponseWriter, r *http.Request) {
	cl := s.clients(r.Context(), actorID(r.Context()))
	hb, err := optional(cl.habits(r.Context(), scopeHabitsRead))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if hb == nil {
		writeJSON(w, http.StatusOK, []pocket.Habit{})
		return
	}
	hs, err := hb.client.Habits(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket Habits: "+err.Error())
		return
	}
	if hs == nil {
		hs = []pocket.Habit{}
	}
	writeJSON(w, http.StatusOK, hs)
}

func (s *Server) handleInventoryLocations(w http.ResponseWriter, r *http.Request) {
	cl := s.clients(r.Context(), actorID(r.Context()))
	inv, err := cl.inventory(r.Context(), scopeItemsRead)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if inv == nil {
		writeJSON(w, http.StatusOK, []pocket.Named{})
		return
	}
	ls, err := inv.client.Locations(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket Inventory: "+err.Error())
		return
	}
	if ls == nil {
		ls = []pocket.Named{}
	}
	writeJSON(w, http.StatusOK, ls)
}

// ---- notifications ----

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	cl := s.clients(r.Context(), actorID(r.Context()))
	if err := cl.notify.Send(r.Context(), notify.Message{Title: "Pocket Kitchen", Body: "Test message.", Priority: "info"}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleNotifyRun(w http.ResponseWriter, r *http.Request) {
	sent, err := s.sendDaily(r.Context(), actorID(r.Context()), true)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "sent": sent})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sent": sent})
}

// sendDaily pushes one user's plan for today and their pantry items about to
// expire to that user's own notification URL.
func (s *Server) sendDaily(ctx context.Context, uid int64, force bool) (int, error) {
	cl := s.clients(ctx, uid)
	if !cl.notify.Configured() {
		return 0, fmt.Errorf("notifications are not configured")
	}
	today := store.Today()
	var lines []string
	if cl.st.NotifyPlan || force {
		entries, _ := s.store.ListPlan(ctx, uid, today, today)
		for _, p := range entries {
			if p.CookedAt != "" {
				continue
			}
			l := cap1(p.Slot) + ": " + p.DisplayTitle()
			if p.RecipeMinutes > 0 {
				l += fmt.Sprintf(" (%d min)", p.RecipeMinutes)
			}
			lines = append(lines, l)
		}
		if len(lines) == 0 && force {
			lines = append(lines, "Nothing planned for today.")
		}
	}
	var expiring []string
	if cl.st.NotifyExpiry || force {
		items, _ := s.store.ListPantry(ctx, uid, store.PantryFilter{Expiring: true})
		for _, p := range items {
			if p.ExpiryDate < today {
				expiring = append(expiring, p.Name+" (expired "+p.ExpiryDate+")")
			} else {
				expiring = append(expiring, p.Name+" (by "+p.ExpiryDate+")")
			}
		}
	}
	open := s.store.OpenItemCount(ctx, uid)
	if len(lines) == 0 && len(expiring) == 0 {
		return 0, nil
	}
	body := strings.Join(lines, "\n")
	if len(expiring) > 0 {
		if body != "" {
			body += "\n\n"
		}
		body += "Use soon: " + strings.Join(expiring, ", ")
	}
	if open > 0 {
		body += fmt.Sprintf("\n\n%d items on the shopping list.", open)
	}
	link := ""
	if s.cfg.BaseURL != "" {
		link = s.cfg.BaseURL + "/plan"
	}
	title := "Today in the kitchen"
	if len(lines) == 0 {
		title = "Pantry: use soon"
	}
	if err := cl.notify.Send(ctx, notify.Message{Title: title, Body: body, Priority: "info", Tags: []string{"fork_and_knife"}, Link: link}); err != nil {
		return 0, err
	}
	return 1, nil
}

func cap1(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ---- dashboard (Homepage widget) & home page ----

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	today := store.Today()
	entries, _ := s.store.ListPlan(ctx, actorID(ctx), today, store.AddDays(today, 1))
	var todayTitles, tomorrowTitles []string
	for _, p := range entries {
		t := p.DisplayTitle()
		if p.Date == today {
			todayTitles = append(todayTitles, t)
		} else {
			tomorrowTitles = append(tomorrowTitles, t)
		}
	}
	join := func(a []string) string {
		if len(a) == 0 {
			return "—"
		}
		return strings.Join(a, " · ")
	}
	counts := s.store.CountsFor(ctx, actorID(ctx))
	total, low, expiring := s.store.PantryStats(ctx, actorID(ctx))
	month, year, days30 := s.store.CookStats(ctx, actorID(ctx))
	spendMonth, _, cur := s.store.SpendStats(ctx, actorID(ctx))
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	if cur == "" {
		cur = st.HomeCurrency
	}
	week, _ := s.store.ListPlan(ctx, actorID(ctx), today, store.AddDays(today, 6))
	writeJSON(w, http.StatusOK, map[string]any{
		"today": join(todayTitles), "today_count": len(todayTitles), "tomorrow": join(tomorrowTitles),
		"recipes": counts["recipes"], "shopping_open": s.store.OpenItemCount(ctx, actorID(ctx)), "pantry_total": total, "pantry_low": low, "pantry_expiring": expiring,
		"cooked_month": month, "cooked_year": year, "cooked_days_30": days30, "planned_week": len(week),
		"spend_month": float64(spendMonth) / pow10(currency.Decimals(cur)), "spend_month_text": majorString(spendMonth, cur), "currency": cur,
	})
}

func pow10(n int) float64 {
	p := 1.0
	for i := 0; i < n; i++ {
		p *= 10
	}
	return p
}

func majorString(minor int64, code string) string {
	d := currency.Decimals(code)
	if d == 0 {
		return fmt.Sprintf("%d %s", minor, code)
	}
	return fmt.Sprintf("%.*f %s", d, float64(minor)/pow10(d), code)
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	today := store.Today()
	week, _ := s.store.ListPlan(ctx, actorID(ctx), today, store.AddDays(today, 6))
	if week == nil {
		week = []store.PlanEntry{}
	}
	lists, _ := s.store.ListLists(ctx, actorID(ctx), false)
	if lists == nil {
		lists = []store.List{}
	}
	cooks, _ := s.store.ListCooks(ctx, actorID(ctx), 0, 6)
	if cooks == nil {
		cooks = []store.Cook{}
	}
	expiring, _ := s.store.ListPantry(ctx, actorID(ctx), store.PantryFilter{Expiring: true})
	if expiring == nil {
		expiring = []store.PantryItem{}
	}
	low, _ := s.store.ListPantry(ctx, actorID(ctx), store.PantryFilter{Low: true})
	if low == nil {
		low = []store.PantryItem{}
	}
	// suggestions: favourites and dishes not cooked for the longest time
	all, _ := s.store.ListRecipes(ctx, actorID(ctx), store.RecipeFilter{})
	type sug struct {
		store.Recipe
		Reason string `json:"reason"`
	}
	var sugs []sug
	sort.SliceStable(all, func(i, j int) bool { return all[i].LastCooked < all[j].LastCooked })
	for _, rc := range all {
		if len(sugs) >= 6 {
			break
		}
		reason := "never cooked"
		if rc.LastCooked != "" {
			reason = "last cooked " + rc.LastCooked
		}
		if rc.Favorite {
			reason = "favourite · " + reason
		}
		sugs = append(sugs, sug{rc, reason})
	}
	if sugs == nil {
		sugs = []sug{}
	}
	total, lowN, expN := s.store.PantryStats(ctx, actorID(ctx))
	month, year, days30 := s.store.CookStats(ctx, actorID(ctx))
	spendMonth, spendYear, cur := s.store.SpendStats(ctx, actorID(ctx))
	if cur == "" {
		cur = st.HomeCurrency
	}
	counts := s.store.CountsFor(ctx, actorID(ctx))
	writeJSON(w, http.StatusOK, map[string]any{
		"today": today, "week": week, "lists": lists, "cooks": cooks, "expiring": expiring, "low": low, "suggestions": sugs,
		"stats": map[string]any{"recipes": counts["recipes"], "pantry": total, "pantry_low": lowN, "pantry_expiring": expN, "cooked_month": month, "cooked_year": year, "cooked_days_30": days30, "shopping_open": s.store.OpenItemCount(ctx, actorID(ctx)), "spend_month": spendMonth, "spend_year": spendYear, "currency": cur},
	})
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"courses": store.Courses, "slots": store.Slots, "aisles": store.Aisles, "units": store.Units})
}

func (s *Server) handleCurrencies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currency.All())
}

// ---- cook log & purchases ----

func (s *Server) handleCookLog(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit")
	if limit <= 0 {
		limit = 100
	}
	cooks, err := s.store.ListCooks(r.Context(), actorID(r.Context()), int64(queryInt(r, "recipe")), limit)
	if err != nil {
		s.fail(w, err, "cook log")
		return
	}
	writeJSON(w, http.StatusOK, cooks)
}

func (s *Server) handleDeleteCook(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteCook(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete cook")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePurchases(w http.ResponseWriter, r *http.Request) {
	ps, err := s.store.ListPurchases(r.Context(), actorID(r.Context()), 100)
	if err != nil {
		s.fail(w, err, "purchases")
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) handleDeletePurchase(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeletePurchase(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete purchase")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- system / backup / export ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size, files int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	_ = filepath.WalkDir(s.cfg.FilesDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				files += fi.Size()
			}
		}
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size, "files_bytes": files,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())),
	})
}

// handleBackup downloads the whole database, so it is a box operation for an
// admin only (everyone can export their own data as JSON).
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-kitchen-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}

// exportDoc is the JSON export of one user's kitchen (no users/tokens).
type exportDoc struct {
	App       string             `json:"app"`
	Version   string             `json:"version"`
	Exported  string             `json:"exported_at"`
	Recipes   []store.Recipe     `json:"recipes"`
	Plan      []store.PlanEntry  `json:"plan"`
	Lists     []store.List       `json:"lists"`
	Items     []store.ListItem   `json:"items"`
	Pantry    []store.PantryItem `json:"pantry"`
	Cooks     []store.Cook       `json:"cooks"`
	Purchases []store.Purchase   `json:"purchases"`
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	doc := exportDoc{App: "pocket-kitchen", Version: Version, Exported: store.Now()}
	doc.Recipes, _ = s.store.ListRecipes(ctx, actorID(ctx), store.RecipeFilter{})
	arch, _ := s.store.ListRecipes(ctx, actorID(ctx), store.RecipeFilter{Archived: true})
	doc.Recipes = append(doc.Recipes, arch...)
	doc.Plan, _ = s.store.ListPlan(ctx, actorID(ctx), "0000-01-01", "9999-12-31")
	doc.Lists, _ = s.store.ListLists(ctx, actorID(ctx), false)
	for _, l := range doc.Lists {
		items, _ := s.store.ListItems(ctx, actorID(ctx), l.ID)
		doc.Items = append(doc.Items, items...)
	}
	doc.Pantry, _ = s.store.ListPantry(ctx, actorID(ctx), store.PantryFilter{})
	doc.Cooks, _ = s.store.ListCooks(ctx, actorID(ctx), 0, 100000)
	doc.Purchases, _ = s.store.ListPurchases(ctx, actorID(ctx), 100000)
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-kitchen-export.json"`)
	writeJSON(w, http.StatusOK, doc)
}

// handleImportJSON merges an export into the caller's own kitchen: recipes by
// title, pantry by name, lists by name; plan entries and cooks are added.
func (s *Server) handleImportJSON(w http.ResponseWriter, r *http.Request) {
	var doc exportDoc
	if err := decode(r, &doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	counts := map[string]int{}
	recipeIDs := map[int64]int64{}
	existing, _ := s.store.ListRecipes(ctx, actorID(ctx), store.RecipeFilter{})
	byTitle := map[string]int64{}
	for _, rc := range existing {
		byTitle[strings.ToLower(rc.Title)] = rc.ID
	}
	for _, rc := range doc.Recipes {
		if id, ok := byTitle[strings.ToLower(rc.Title)]; ok {
			recipeIDs[rc.ID] = id
			continue
		}
		old := rc.ID
		rc.ID, rc.PhotoPath = 0, ""
		if uid > 0 {
			rc.CreatedBy = &uid
		}
		for i := range rc.Ingredients {
			rc.Ingredients[i].PantryID = 0
		}
		if err := s.store.CreateRecipe(ctx, actorID(ctx), &rc); err == nil {
			recipeIDs[old] = rc.ID
			byTitle[strings.ToLower(rc.Title)] = rc.ID
			counts["recipes"]++
		}
	}
	for _, p := range doc.Pantry {
		if _, err := s.store.FindPantryByName(ctx, actorID(ctx), p.Name); err == nil {
			continue
		}
		p.ID, p.InventoryID = 0, nil
		if err := s.store.CreatePantry(ctx, actorID(ctx), &p); err == nil {
			counts["pantry"]++
		}
	}
	listIDs := map[int64]int64{}
	lists, _ := s.store.ListLists(ctx, actorID(ctx), false)
	for _, l := range doc.Lists {
		found := false
		for _, ex := range lists {
			if strings.EqualFold(ex.Name, l.Name) {
				listIDs[l.ID] = ex.ID
				found = true
			}
		}
		if !found {
			old := l.ID
			l.ID = 0
			if err := s.store.CreateList(ctx, actorID(ctx), &l); err == nil {
				listIDs[old] = l.ID
				counts["lists"]++
			}
		}
	}
	for _, it := range doc.Items {
		lid, ok := listIDs[it.ListID]
		if !ok {
			continue
		}
		it.ID, it.ListID, it.PlanID, it.PantryID, it.CreatedBy = 0, lid, nil, nil, nil
		if uid > 0 {
			it.CreatedBy = &uid
		}
		if it.RecipeID != nil {
			if nid, ok := recipeIDs[*it.RecipeID]; ok {
				it.RecipeID = &nid
			} else {
				it.RecipeID = nil
			}
		}
		if err := s.store.CreateItem(ctx, actorID(ctx), &it); err == nil {
			counts["items"]++
		}
	}
	for _, p := range doc.Plan {
		p.ID, p.CookID, p.CreatedBy = 0, nil, nil
		if uid > 0 {
			p.CreatedBy = &uid
		}
		if p.RecipeID != nil {
			if nid, ok := recipeIDs[*p.RecipeID]; ok {
				p.RecipeID = &nid
			} else {
				p.RecipeID = nil
				if p.Title == "" {
					p.Title = p.RecipeTitle
				}
			}
		}
		if p.RecipeID == nil && p.Title == "" {
			continue
		}
		if err := s.store.CreatePlan(ctx, actorID(ctx), &p); err == nil {
			counts["plan"]++
		}
	}
	for _, c := range doc.Cooks {
		c.ID, c.PlanID = 0, nil
		if c.RecipeID != nil {
			if nid, ok := recipeIDs[*c.RecipeID]; ok {
				c.RecipeID = &nid
			} else {
				c.RecipeID = nil
			}
		}
		if err := s.store.CreateCook(ctx, actorID(ctx), &c); err == nil {
			counts["cooks"]++
			if c.RecipeID != nil {
				_ = s.store.MarkCooked(ctx, actorID(ctx), *c.RecipeID, c.Date)
			}
		}
	}
	for _, p := range doc.Purchases {
		p.ID, p.ListID = 0, nil
		if err := s.store.CreatePurchase(ctx, actorID(ctx), &p); err == nil {
			counts["purchases"]++
		}
	}
	writeJSON(w, http.StatusOK, counts)
}

var _ = units.Format

// coreStatus summarises the Pocket Core link for the About page.
func (s *Server) coreStatus() map[string]any {
	s.coreMu.Lock()
	defer s.coreMu.Unlock()
	return map[string]any{"configured": s.core.Configured(), "url": s.cfg.CoreURL, "registered": s.coreOK, "error": s.coreErr}
}
