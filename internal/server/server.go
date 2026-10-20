// Package server wires the HTTP API, auth and the embedded SPA.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/crypto/bcrypt"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/config"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/foodfacts"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
)

// Version is injected at build time.
var Version = "dev"

// Server holds dependencies.
type Server struct {
	cfg      *config.Config
	store    *store.Store
	log      *slog.Logger
	webFS    fs.FS
	started  time.Time
	requests atomic.Int64
	errors   atomic.Int64
	off      *foodfacts.Client

	syncMu    sync.Mutex
	syncing   map[int64]bool   // per user: a pantry sync is running
	notifyDay map[int64]string // per user: the day the daily push went out

	oidcMu sync.Mutex
	oidc   *oidcProvider // for the issuer in use; rebuilt when it changes

	core     *core.Client
	coreMu   sync.Mutex
	coreOK   bool
	coreErr  string
	resolved map[string]resolvedURL
	modesMu  sync.Mutex // one connection-mode report at a time
}

type resolvedURL struct {
	url string
	at  time.Time
}

// New creates a server.
func New(cfg *config.Config, st *store.Store, log *slog.Logger, webFS fs.FS) *Server {
	if cfg.DefaultCurrency != "" {
		st.DefaultCurrency = cfg.DefaultCurrency
	}
	return &Server{cfg: cfg, store: st, log: log, webFS: webFS, started: time.Now(), off: foodfacts.New(cfg.UserAgent), oidc: &oidcProvider{},
		core:     core.New(cfg.CoreURL, core.Options{DB: st.DB, TokenFile: cfg.DataDir + "/core.token", UserAgent: cfg.UserAgent}),
		resolved: map[string]resolvedURL{}, syncing: map[int64]bool{}, notifyDay: map[int64]string{}}
}

// Bootstrap seeds the first admin (if configured), the owner's integration
// settings from the environment and demo data, then starts the background work.
func (s *Server) Bootstrap(ctx context.Context) error {
	if err := s.core.Start(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll(s.cfg.FilesDir, 0o755); err != nil {
		return fmt.Errorf("files dir: %w", err)
	}
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 0 && s.cfg.AuthMode == "local" && s.cfg.AdminEmail != "" && s.cfg.AdminPassword != "" {
		if len(s.cfg.AdminPassword) < 8 {
			return errors.New("ADMIN_PASSWORD must be at least 8 characters")
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(s.cfg.AdminPassword), bcrypt.DefaultCost)
		u := &store.User{Email: s.cfg.AdminEmail, Name: s.cfg.AdminName, PasswordHash: string(h), Role: "admin"}
		if err := s.store.CreateUser(ctx, u); err != nil {
			return err
		}
		st, _ := s.store.GetSettings(ctx, 0)
		st.SetupComplete = true
		if err := s.store.SaveBoxSettings(ctx, st); err != nil {
			return err
		}
		s.log.Info("admin user created from environment", "email", u.Email)
	}
	box, err := s.store.GetSettings(ctx, 0)
	if err != nil {
		return err
	}
	if !s.cfg.FoodFactsEnabled && box.FoodFacts {
		box.FoodFacts = false
		if err := s.store.SaveBoxSettings(ctx, box); err != nil {
			return err
		}
	}
	if owner := s.store.OwnerID(ctx); owner > 0 {
		if err := s.ownerReady(ctx, owner); err != nil {
			return err
		}
	}
	_ = s.store.PurgeSessions(ctx)
	go s.janitor(ctx)
	if s.core.Configured() {
		go s.coreLoop(ctx)
	}
	return nil
}

// ownerReady runs once the box owner (the first admin) exists: the integration
// settings from the environment (Inventory / Split / Habits tokens and ids, the
// Apprise route) are the owner's — they are seeded into the owner's personal
// settings only, never into anyone else's — and demo data lands in the owner's
// kitchen.
func (s *Server) ownerReady(ctx context.Context, owner int64) error {
	st, err := s.store.GetSettings(ctx, owner)
	if err != nil {
		return err
	}
	changed := false
	// Each value is copied once: clearing it in Settings (Disconnect, to move
	// Inventory, Split or Habits over to Pocket Core) must not bring it back
	// at the next start. A value already there counts as copied.
	once := func(name string, empty bool, v string, apply func()) {
		key := fmt.Sprintf("seeded:%d:%s", owner, name)
		if _, _, done := s.store.CacheGet(ctx, key); done {
			return
		}
		if empty && v != "" {
			apply()
			changed = true
			empty = false
		}
		if !empty {
			_ = s.store.CachePut(ctx, key, "1")
		}
	}
	set := func(name string, dst *string, v string) {
		once(name, *dst == "", v, func() { *dst = v })
	}
	set("NotifyURL", &st.NotifyURL, s.cfg.NotifyURL)
	set("InventoryURL", &st.InventoryURL, s.cfg.InventoryURL)
	set("InventoryToken", &st.InventoryToken, s.cfg.InventoryToken)
	set("SplitURL", &st.SplitURL, s.cfg.SplitURL)
	set("SplitToken", &st.SplitToken, s.cfg.SplitToken)
	set("SplitPublicURL", &st.SplitPublicURL, s.cfg.SplitPublicURL)
	set("HabitsURL", &st.HabitsURL, s.cfg.HabitsURL)
	set("HabitsToken", &st.HabitsToken, s.cfg.HabitsToken)
	if s.cfg.InventoryTag != "" && s.cfg.InventoryTag != "pantry" && st.InventoryTag == "pantry" {
		st.InventoryTag = s.cfg.InventoryTag
		changed = true
	}
	if s.cfg.SplitGroupID > 0 {
		once("SplitGroupID", st.SplitGroupID == 0, "1", func() { st.SplitGroupID = s.cfg.SplitGroupID })
	}
	if s.cfg.HabitID > 0 {
		once("HabitID", st.HabitID == 0, "1", func() { st.HabitID = s.cfg.HabitID })
	}
	if s.cfg.NotifyFormat != "" && st.NotifyURL == s.cfg.NotifyURL && st.NotifyFormat != s.cfg.NotifyFormat {
		st.NotifyFormat = s.cfg.NotifyFormat
		changed = true
	}
	if s.cfg.NotifyHour != 9 && st.NotifyHour == 9 {
		st.NotifyHour = s.cfg.NotifyHour
		changed = true
	}
	if changed {
		if err := s.store.SaveUserSettings(ctx, owner, st); err != nil {
			return err
		}
	}
	if s.cfg.DemoData {
		if err := s.seedDemo(ctx, owner); err != nil {
			return err
		}
	}
	return nil
}

// janitor runs the background work: pantry sync every 30 minutes, the daily
// push once the configured hour has passed.
func (s *Server) janitor(ctx context.Context) {
	first := time.NewTimer(15 * time.Second)
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			s.autoSync(ctx)
			s.maybeNotify(ctx)
		case <-t.C:
			s.autoSync(ctx)
			s.maybeNotify(ctx)
		}
	}
}

// autoSync pulls the pantry for every person connected to their own Pocket
// Inventory (their own token, or a Core link acting as them) into their own
// pantry. People without a connection are skipped quietly.
func (s *Server) autoSync(ctx context.Context) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		s.log.Warn("pantry sync: users", "err", err)
		return
	}
	for _, u := range users {
		st, _ := s.store.GetSettings(ctx, u.ID)
		if !st.InventoryAutoSync {
			continue
		}
		switch s.connMode(st.InventoryMode, st.InventoryToken) {
		case modeOff:
			continue
		case modeDirect:
			if st.InventoryToken == "" {
				continue
			}
		}
		if _, err := s.syncPantry(ctx, u.ID); err != nil {
			if isNotConnected(err) {
				s.log.Debug("pantry sync skipped", "user", u.ID, "why", err)
				continue
			}
			s.log.Warn("pantry sync", "user", u.ID, "err", err)
		}
	}
}

// maybeNotify sends each user's daily message (to their own notification URL)
// once per day once their hour has passed.
func (s *Server) maybeNotify(ctx context.Context) {
	uids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	day := now.Format("2006-01-02")
	for _, uid := range uids {
		st, _ := s.store.GetSettings(ctx, uid)
		if st.NotifyURL == "" || (!st.NotifyPlan && !st.NotifyExpiry) {
			continue
		}
		key := fmt.Sprintf("notify:last_day:%d", uid)
		last := s.notifyDay[uid]
		if last == "" {
			if v, _, ok := s.store.CacheGet(ctx, key); ok {
				last = v
			}
		}
		if last == day || now.Hour() < st.NotifyHour {
			s.notifyDay[uid] = last
			continue
		}
		s.notifyDay[uid] = day
		_ = s.store.CachePut(ctx, key, day)
		if _, err := s.sendDaily(ctx, uid, false); err != nil {
			s.log.Warn("daily notification", "user", uid, "err", err)
		}
	}
}

// Router builds the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	if s.cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.RequestID)
	r.Use(s.logging)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(middleware.Timeout(120 * time.Second))

	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Get("/api/health", s.handleHealth)
	if s.cfg.MetricsEnabled {
		r.Get("/metrics", s.handleMetrics)
	}

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/setup", s.handleSetup)
		r.Get("/auth/status", s.handleAuthStatus)
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Put("/auth/me", s.handleUpdateMe)

			r.Get("/dashboard", s.handleDashboard)
			r.Get("/home", s.handleHome)
			r.Get("/meta", s.handleMeta)
			r.Get("/currencies", s.handleCurrencies)
			r.Get("/search", s.handleSearch)

			r.Get("/recipes", s.handleListRecipes)
			r.Post("/recipes", s.handleCreateRecipe)
			r.Post("/recipes/import", s.handleImportRecipe)
			r.Get("/recipes/facets", s.handleRecipeFacets)
			r.Get("/recipes/{id}", s.handleGetRecipe)
			r.Put("/recipes/{id}", s.handleUpdateRecipe)
			r.Delete("/recipes/{id}", s.handleDeleteRecipe)
			r.Post("/recipes/{id}/favorite", s.handleFavoriteRecipe)
			r.Post("/recipes/{id}/cook", s.handleCookRecipe)
			r.Post("/recipes/{id}/shop", s.handleRecipeToList)
			r.Post("/recipes/{id}/duplicate", s.handleDuplicateRecipe)
			r.Get("/recipes/{id}/photo", s.handleRecipePhoto)
			r.Post("/recipes/{id}/photo", s.handleUploadRecipePhoto)
			r.Delete("/recipes/{id}/photo", s.handleDeleteRecipePhoto)
			r.Get("/recipes/{id}/export.md", s.handleRecipeMarkdown)

			r.Get("/plan", s.handleListPlan)
			r.Post("/plan", s.handleCreatePlan)
			r.Post("/plan/shop", s.handlePlanToList)
			r.Post("/plan/copy", s.handleCopyPlan)
			r.Put("/plan/{id}", s.handleUpdatePlan)
			r.Delete("/plan/{id}", s.handleDeletePlan)
			r.Post("/plan/{id}/cook", s.handleCookPlan)

			r.Get("/lists", s.handleListLists)
			r.Post("/lists", s.handleCreateList)
			r.Get("/lists/suggest", s.handleSuggestItems)
			r.Get("/lists/{id}", s.handleGetList)
			r.Put("/lists/{id}", s.handleUpdateList)
			r.Delete("/lists/{id}", s.handleDeleteList)
			r.Post("/lists/{id}/items", s.handleCreateItem)
			r.Put("/lists/{id}/items/{iid}", s.handleUpdateItem)
			r.Delete("/lists/{id}/items/{iid}", s.handleDeleteItem)
			r.Post("/lists/{id}/items/{iid}/toggle", s.handleToggleItem)
			r.Post("/lists/{id}/check", s.handleCheckAll)
			r.Post("/lists/{id}/clear", s.handleClearChecked)
			r.Post("/lists/{id}/checkout", s.handleCheckout)
			r.Post("/lists/{id}/low", s.handleAddLowPantry)

			r.Get("/pantry", s.handleListPantry)
			r.Post("/pantry", s.handleCreatePantry)
			r.Post("/pantry/sync", s.handleSyncPantry)
			r.Get("/pantry/facets", s.handlePantryFacets)
			r.Get("/pantry/{id}", s.handleGetPantry)
			r.Put("/pantry/{id}", s.handleUpdatePantry)
			r.Delete("/pantry/{id}", s.handleDeletePantry)
			r.Post("/pantry/{id}/adjust", s.handleAdjustPantry)
			r.Get("/lookup", s.handleLookup)

			r.Get("/cooklog", s.handleCookLog)
			r.Delete("/cooklog/{id}", s.handleDeleteCook)
			r.Get("/purchases", s.handlePurchases)
			r.Delete("/purchases/{id}", s.handleDeletePurchase)

			r.Post("/integrations/test", s.handleIntegrationTest)
			r.Get("/split/groups", s.handleSplitGroups)
			r.Get("/split/categories", s.handleSplitCategories)
			r.Get("/habits/list", s.handleHabitsList)
			r.Get("/inventory/locations", s.handleInventoryLocations)
			r.Post("/notify/test", s.handleNotifyTest)
			r.Post("/notify/run", s.handleNotifyRun)

			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)

			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handlePutSettings)
			r.Get("/settings/system", s.handleSystemInfo)

			r.Get("/tokens", s.handleListTokens)
			r.Post("/tokens", s.handleCreateToken)
			r.Delete("/tokens/{id}", s.handleDeleteToken)

			r.Get("/backup", s.handleBackup)
			r.Get("/export.json", s.handleExportJSON)
			r.Post("/import", s.handleImportJSON)
		})
	})

	r.NotFound(s.handleSPA)
	return r
}

// ---------- helpers ----------

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.requests.Add(1)
		if ww.Status() >= 500 {
			s.errors.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			return
		}
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "dur", time.Since(start).String(), "ip", r.RemoteAddr)
	})
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

func (s *Server) fail(w http.ResponseWriter, err error, context string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "already exists")
		return
	}
	s.log.Error(context, "err", err)
	writeErr(w, http.StatusInternalServerError, context+": "+err.Error())
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 32<<20))
	return dec.Decode(v)
}

// decodeKeys decodes JSON into v and also reports which top-level keys were
// present, so PUT handlers can tell "not sent" from "sent empty".
func decodeKeys(r *http.Request, v any) (map[string]bool, error) {
	b, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(raw))
	for k := range raw {
		keys[k] = true
	}
	return keys, json.Unmarshal(b, v)
}

func idParam(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return n
}

func queryInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

func queryBool(r *http.Request, name string) bool {
	v := strings.ToLower(r.URL.Query().Get(name))
	return v == "1" || v == "true" || v == "yes"
}

func validDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// ---------- health / metrics ----------

// ---- Pocket Core ----

// coreLoop registers with Core on start (retrying until it answers) and
// re-registers every 30 minutes so version and last-seen stay fresh; each
// time it also reports how everybody connects (modes.go).
func (s *Server) coreLoop(ctx context.Context) {
	wait := 5 * time.Second
	for {
		err := s.registerWithCore(ctx)
		s.coreMu.Lock()
		s.coreOK, s.coreErr = err == nil, ""
		if err != nil {
			s.coreErr = err.Error()
		}
		s.coreMu.Unlock()
		if err != nil {
			s.log.Warn("pocket core: registration failed, retrying", "err", err, "in", wait.String())
		} else {
			wait = 30 * time.Minute
			s.reportModes(ctx) // on start and with every re-registration (30 min)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err != nil && wait < 5*time.Minute {
			wait *= 2
		}
	}
}

func (s *Server) registerWithCore(ctx context.Context) error {
	m := core.Manifest{
		Slug: "kitchen", Name: "Pocket Kitchen",
		Description: "Recipes, the week's meal plan, the shopping list and the pantry.",
		Icon:        "/favicon.svg", Version: Version,
		URL: s.cfg.CoreAppURL, PublicURL: s.cfg.BaseURL, HealthPath: "/healthz", EnvPrefix: "KITCHEN",
		Publishes: []string{"kitchen.cooked", "kitchen.checkout"},
		Offers:    linkOffers,
		Uses:      linkUses(),
	}
	if s.cfg.BaseURL != "" {
		// ask Core for a relying-party client so "Sign in with Pocket Core" needs no configuration
		m.OIDC = &core.OIDCRequest{RedirectURIs: []string{s.oidcCallbackURL()}}
	}
	reg, err := s.core.Register(ctx, s.cfg.CoreRegistrationKey, m)
	if err != nil {
		return err
	}
	if reg.Token != "" {
		s.log.Info("pocket core: registered, app token stored", "core", s.cfg.CoreURL)
	} else {
		s.log.Debug("pocket core: re-registered", "core", s.cfg.CoreURL)
	}
	return nil
}

// publish persists an event locally; the SDK retries network delivery.
func (s *Server) publish(topic, subject string, payload map[string]any) {
	if !s.core.Configured() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.core.Publish(ctx, topic, subject, payload); err != nil {
		s.log.Warn("pocket core: outbox enqueue failed", "topic", topic, "err", err)
	}
}

// resolveSibling asks Core where a sibling app lives (cached a minute) so the
// *_URL settings may stay empty on a box with Core.
func (s *Server) resolveSibling(ctx context.Context, slug string) string {
	if !s.core.Configured() {
		return ""
	}
	s.coreMu.Lock()
	if r, ok := s.resolved[slug]; ok && time.Since(r.at) < time.Minute {
		s.coreMu.Unlock()
		return r.url
	}
	s.coreMu.Unlock()
	url := ""
	if res, err := s.core.Resolve(ctx, slug); err == nil {
		url = res.URL
	}
	s.coreMu.Lock()
	s.resolved[slug] = resolvedURL{url: url, at: time.Now()}
	s.coreMu.Unlock()
	return url
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"core_outbox": s.coreOutboxHealth(r.Context()), "status": "ok", "version": Version, "uptime": time.Since(s.started).Round(time.Second).String()})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts := s.store.Counts(ctx)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP pocket_kitchen_info Build info.\n# TYPE pocket_kitchen_info gauge\npocket_kitchen_info{version=%q} 1\n", Version)
	fmt.Fprintf(w, "# HELP pocket_kitchen_uptime_seconds Process uptime.\n# TYPE pocket_kitchen_uptime_seconds gauge\npocket_kitchen_uptime_seconds %d\n", int(time.Since(s.started).Seconds()))
	fmt.Fprintf(w, "# HELP pocket_kitchen_http_requests_total HTTP requests served.\n# TYPE pocket_kitchen_http_requests_total counter\npocket_kitchen_http_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# HELP pocket_kitchen_http_errors_total HTTP 5xx responses.\n# TYPE pocket_kitchen_http_errors_total counter\npocket_kitchen_http_errors_total %d\n", s.errors.Load())
	fmt.Fprintf(w, "# HELP pocket_kitchen_rows Rows per table.\n# TYPE pocket_kitchen_rows gauge\n")
	for k, v := range counts {
		fmt.Fprintf(w, "pocket_kitchen_rows{table=%q} %d\n", k, v)
	}
	fmt.Fprintf(w, "# HELP pocket_kitchen_shopping_open Unchecked shopping lines.\n# TYPE pocket_kitchen_shopping_open gauge\npocket_kitchen_shopping_open %d\n", s.store.OpenItemCountAll(ctx))
}

// ---------- static / SPA ----------

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if s.webFS == nil {
		http.Error(w, "web UI not built (run `make web`)", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := s.webFS.Open(path); err == nil {
		f.Close()
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServerFS(s.webFS).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	http.FileServerFS(s.webFS).ServeHTTP(w, r)
}

// ---------- files ----------

// saveFile writes bytes under FILES_DIR/<sub>/ with a random name and returns the relative path.
func (s *Server) saveFile(sub, ext string, data []byte) (string, error) {
	dir := filepath.Join(s.cfg.FilesDir, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext = strings.ToLower(filepath.Ext("x" + ext))
	if len(ext) > 8 {
		ext = ""
	}
	name := store.RandomToken(12) + ext
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return "", err
	}
	return sub + "/" + name, nil
}

func (s *Server) filePath(rel string) string {
	rel = filepath.Clean("/" + rel)
	return filepath.Join(s.cfg.FilesDir, rel)
}

func (s *Server) removeFile(rel string) {
	if rel == "" {
		return
	}
	_ = os.Remove(s.filePath(rel))
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, rel string) {
	if rel == "" {
		writeErr(w, http.StatusNotFound, "no file")
		return
	}
	p := s.filePath(rel)
	if _, err := os.Stat(p); err != nil {
		writeErr(w, http.StatusNotFound, "file missing")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, p)
}

// readUpload reads one multipart file field (or the raw body) up to the limit.
func (s *Server) readUpload(r *http.Request, field string) ([]byte, string, string, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, s.cfg.MaxUploadBytes)
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(s.cfg.MaxUploadBytes); err != nil {
			return nil, "", "", err
		}
		f, hdr, err := r.FormFile(field)
		if err != nil {
			return nil, "", "", err
		}
		defer f.Close()
		b, err := io.ReadAll(f)
		if err != nil {
			return nil, "", "", err
		}
		return b, hdr.Filename, hdr.Header.Get("Content-Type"), nil
	}
	b, err := io.ReadAll(r.Body)
	return b, r.URL.Query().Get("name"), ct, err
}
