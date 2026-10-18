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

func (s *Server) publicSettings(st store.Settings) map[string]any {
	return map[string]any{
		"title": st.Title, "home_currency": st.HomeCurrency, "date_format": st.DateFormat, "time_format": st.TimeFormat, "week_start": st.WeekStart, "default_servings": st.DefaultServings,
		"notify_url": st.NotifyURL, "notify_format": st.NotifyFormat, "notify_hour": st.NotifyHour, "notify_plan": st.NotifyPlan, "notify_expiry": st.NotifyExpiry, "notify_configured": st.NotifyURL != "",
		"inventory_url": st.InventoryURL, "inventory_token": mask(st.InventoryToken), "inventory_tag": st.InventoryTag, "inventory_location": st.InventoryLocation, "inventory_auto_sync": st.InventoryAutoSync,
		"inventory_configured": st.InventoryURL != "" && st.InventoryToken != "",
		"split_url":            st.SplitURL, "split_token": mask(st.SplitToken), "split_public_url": st.SplitPublicURL, "split_group_id": st.SplitGroupID, "split_member_id": st.SplitMemberID, "split_category": st.SplitCategory,
		"split_configured": st.SplitURL != "" && st.SplitToken != "" && st.SplitGroupID > 0,
		"habits_url":       st.HabitsURL, "habits_token": mask(st.HabitsToken), "habit_id": st.HabitID,
		"habits_configured": st.HabitsURL != "" && st.HabitsToken != "" && st.HabitID > 0,
		"food_facts":        st.FoodFacts, "max_upload_bytes": s.cfg.MaxUploadBytes, "setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL,
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context())
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(st))
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	cur, err := s.store.GetSettings(r.Context())
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
	if keys["title"] {
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
	if keys["food_facts"] {
		cur.FoodFacts = in.FoodFacts
	}
	if err := s.store.SaveSettings(r.Context(), cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(cur))
}

func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
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
	st, _ := s.store.GetSettings(r.Context())
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
	case "inventory":
		c := pocket.New(urlOr(st.InventoryURL), keyOr(st.InventoryToken), 15*time.Second)
		var v string
		v, err = c.Health(ctx)
		if err == nil {
			var items []pocket.Item
			items, err = c.ItemsByTag(ctx, st.InventoryTag)
			msg = fmt.Sprintf("Pocket Inventory %s, %d items tagged “%s”", v, len(items), st.InventoryTag)
		}
	case "split":
		c := pocket.New(urlOr(st.SplitURL), keyOr(st.SplitToken), 15*time.Second)
		var v string
		v, err = c.Health(ctx)
		if err == nil {
			var gs []pocket.Group
			gs, err = c.Groups(ctx)
			msg = fmt.Sprintf("Pocket Split %s, %d groups", v, len(gs))
		}
	case "habits":
		c := pocket.New(urlOr(st.HabitsURL), keyOr(st.HabitsToken), 15*time.Second)
		var v string
		v, err = c.Health(ctx)
		if err == nil {
			var hs []pocket.Habit
			hs, err = c.Habits(ctx)
			msg = fmt.Sprintf("Pocket Habits %s, %d habits", v, len(hs))
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

func (s *Server) handleSplitGroups(w http.ResponseWriter, r *http.Request) {
	cl := s.clients(r.Context())
	if !cl.split.Configured() {
		writeJSON(w, http.StatusOK, []pocket.Group{})
		return
	}
	gs, err := cl.split.Groups(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket Split: "+err.Error())
		return
	}
	// groups come without members in the list; fetch the chosen one (and the rest lazily)
	out := make([]pocket.Group, 0, len(gs))
	for _, g := range gs {
		if g.Archived {
			continue
		}
		if len(g.Members) == 0 {
			if full, err := cl.split.Group(r.Context(), g.ID); err == nil {
				g = *full
			}
		}
		out = append(out, g)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSplitCategories(w http.ResponseWriter, r *http.Request) {
	cl := s.clients(r.Context())
	if !cl.split.Configured() {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	cats, err := cl.split.Categories(r.Context())
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
	cl := s.clients(r.Context())
	if !cl.habits.Configured() {
		writeJSON(w, http.StatusOK, []pocket.Habit{})
		return
	}
	hs, err := cl.habits.Habits(r.Context())
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
	cl := s.clients(r.Context())
	if !cl.inventory.Configured() {
		writeJSON(w, http.StatusOK, []pocket.Named{})
		return
	}
	ls, err := cl.inventory.Locations(r.Context())
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
	cl := s.clients(r.Context())
	if err := cl.notify.Send(r.Context(), notify.Message{Title: "Pocket Kitchen", Body: "Test message.", Priority: "info"}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleNotifyRun(w http.ResponseWriter, r *http.Request) {
	sent, err := s.sendDaily(r.Context(), true)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "sent": sent})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sent": sent})
}

// sendDaily pushes today's plan and the pantry items about to expire.
func (s *Server) sendDaily(ctx context.Context, force bool) (int, error) {
	cl := s.clients(ctx)
	if !cl.notify.Configured() {
		return 0, fmt.Errorf("notifications are not configured")
	}
	today := store.Today()
	var lines []string
	if cl.st.NotifyPlan || force {
		entries, _ := s.store.ListPlan(ctx, today, today)
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
		items, _ := s.store.ListPantry(ctx, store.PantryFilter{Expiring: true})
		for _, p := range items {
			if p.ExpiryDate < today {
				expiring = append(expiring, p.Name+" (expired "+p.ExpiryDate+")")
			} else {
				expiring = append(expiring, p.Name+" (by "+p.ExpiryDate+")")
			}
		}
	}
	open := s.store.OpenItemCount(ctx)
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
	entries, _ := s.store.ListPlan(ctx, today, store.AddDays(today, 1))
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
	counts := s.store.Counts(ctx)
	total, low, expiring := s.store.PantryStats(ctx)
	month, year, days30 := s.store.CookStats(ctx)
	spendMonth, _, cur := s.store.SpendStats(ctx)
	st, _ := s.store.GetSettings(ctx)
	if cur == "" {
		cur = st.HomeCurrency
	}
	week, _ := s.store.ListPlan(ctx, today, store.AddDays(today, 6))
	writeJSON(w, http.StatusOK, map[string]any{
		"today": join(todayTitles), "today_count": len(todayTitles), "tomorrow": join(tomorrowTitles),
		"recipes": counts["recipes"], "shopping_open": s.store.OpenItemCount(ctx), "pantry_total": total, "pantry_low": low, "pantry_expiring": expiring,
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
	st, _ := s.store.GetSettings(ctx)
	today := store.Today()
	week, _ := s.store.ListPlan(ctx, today, store.AddDays(today, 6))
	if week == nil {
		week = []store.PlanEntry{}
	}
	lists, _ := s.store.ListLists(ctx, false)
	if lists == nil {
		lists = []store.List{}
	}
	cooks, _ := s.store.ListCooks(ctx, 0, 6)
	if cooks == nil {
		cooks = []store.Cook{}
	}
	expiring, _ := s.store.ListPantry(ctx, store.PantryFilter{Expiring: true})
	if expiring == nil {
		expiring = []store.PantryItem{}
	}
	low, _ := s.store.ListPantry(ctx, store.PantryFilter{Low: true})
	if low == nil {
		low = []store.PantryItem{}
	}
	// suggestions: favourites and dishes not cooked for the longest time
	all, _ := s.store.ListRecipes(ctx, store.RecipeFilter{})
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
	total, lowN, expN := s.store.PantryStats(ctx)
	month, year, days30 := s.store.CookStats(ctx)
	spendMonth, spendYear, cur := s.store.SpendStats(ctx)
	if cur == "" {
		cur = st.HomeCurrency
	}
	counts := s.store.Counts(ctx)
	writeJSON(w, http.StatusOK, map[string]any{
		"today": today, "week": week, "lists": lists, "cooks": cooks, "expiring": expiring, "low": low, "suggestions": sugs,
		"stats": map[string]any{"recipes": counts["recipes"], "pantry": total, "pantry_low": lowN, "pantry_expiring": expN, "cooked_month": month, "cooked_year": year, "cooked_days_30": days30, "shopping_open": s.store.OpenItemCount(ctx), "spend_month": spendMonth, "spend_year": spendYear, "currency": cur},
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
	cooks, err := s.store.ListCooks(r.Context(), int64(queryInt(r, "recipe")), limit)
	if err != nil {
		s.fail(w, err, "cook log")
		return
	}
	writeJSON(w, http.StatusOK, cooks)
}

func (s *Server) handleDeleteCook(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteCook(r.Context(), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete cook")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePurchases(w http.ResponseWriter, r *http.Request) {
	ps, err := s.store.ListPurchases(r.Context(), 100)
	if err != nil {
		s.fail(w, err, "purchases")
		return
	}
	writeJSON(w, http.StatusOK, ps)
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
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size, "files_bytes": files,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.Counts(r.Context()),
	})
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
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

// exportDoc is the JSON backup of everything but users/tokens.
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
	doc.Recipes, _ = s.store.ListRecipes(ctx, store.RecipeFilter{})
	arch, _ := s.store.ListRecipes(ctx, store.RecipeFilter{Archived: true})
	doc.Recipes = append(doc.Recipes, arch...)
	doc.Plan, _ = s.store.ListPlan(ctx, "0000-01-01", "9999-12-31")
	doc.Lists, _ = s.store.ListLists(ctx, false)
	for _, l := range doc.Lists {
		items, _ := s.store.ListItems(ctx, l.ID)
		doc.Items = append(doc.Items, items...)
	}
	doc.Pantry, _ = s.store.ListPantry(ctx, store.PantryFilter{})
	doc.Cooks, _ = s.store.ListCooks(ctx, 0, 100000)
	doc.Purchases, _ = s.store.ListPurchases(ctx, 100000)
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-kitchen-export.json"`)
	writeJSON(w, http.StatusOK, doc)
}

// handleImportJSON merges an export: recipes by title, pantry by name,
// lists by name; plan entries and cooks are added when not already present.
func (s *Server) handleImportJSON(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var doc exportDoc
	if err := decode(r, &doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	counts := map[string]int{}
	recipeIDs := map[int64]int64{}
	existing, _ := s.store.ListRecipes(ctx, store.RecipeFilter{})
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
		if err := s.store.CreateRecipe(ctx, &rc); err == nil {
			recipeIDs[old] = rc.ID
			byTitle[strings.ToLower(rc.Title)] = rc.ID
			counts["recipes"]++
		}
	}
	for _, p := range doc.Pantry {
		if _, err := s.store.FindPantryByName(ctx, p.Name); err == nil {
			continue
		}
		p.ID, p.InventoryID = 0, nil
		if err := s.store.CreatePantry(ctx, &p); err == nil {
			counts["pantry"]++
		}
	}
	listIDs := map[int64]int64{}
	lists, _ := s.store.ListLists(ctx, false)
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
			if err := s.store.CreateList(ctx, &l); err == nil {
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
		it.ID, it.ListID, it.PlanID, it.PantryID = 0, lid, nil, nil
		if it.RecipeID != nil {
			if nid, ok := recipeIDs[*it.RecipeID]; ok {
				it.RecipeID = &nid
			} else {
				it.RecipeID = nil
			}
		}
		if err := s.store.CreateItem(ctx, &it); err == nil {
			counts["items"]++
		}
	}
	for _, p := range doc.Plan {
		p.ID, p.CookID = 0, nil
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
		if err := s.store.CreatePlan(ctx, &p); err == nil {
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
		if err := s.store.CreateCook(ctx, &c); err == nil {
			counts["cooks"]++
			if c.RecipeID != nil {
				_ = s.store.MarkCooked(ctx, *c.RecipeID, c.Date)
			}
		}
	}
	for _, p := range doc.Purchases {
		p.ID, p.ListID = 0, nil
		if err := s.store.CreatePurchase(ctx, &p); err == nil {
			counts["purchases"]++
		}
	}
	writeJSON(w, http.StatusOK, counts)
}

var _ = units.Format
