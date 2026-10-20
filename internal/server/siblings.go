package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
)

// ---- Pocket Inventory, Split and Habits ----
//
// Each person chooses per app how Kitchen reaches it: through a Pocket Core
// app link (Kitchen asks Core for a five-minute token acting as that person,
// nothing to paste), directly with their own URL + API token, or not at all.
// Core tokens are asked for per request with only the scopes the work needs
// and never kept, so a link switched off in Core applies at once.

// Scopes Kitchen uses in the other apps (the manifest's Uses).
const (
	scopeItemsRead     = "items:read"
	scopeItemsWrite    = "items:write"
	scopeItemsAdjust   = "items:adjust"
	scopeGroupsRead    = "groups:read"
	scopeExpensesWrite = "expenses:write"
	scopeHabitsRead    = "habits:read"
	scopeEntriesWrite  = "entries:write"
)

// pocketApp is one sibling Pocket app Kitchen acts in.
type pocketApp struct {
	slug, name string
	scopes     []string // everything Kitchen may ask for there
	reason     string   // shown in Pocket Core
}

var (
	appInventory = pocketApp{"inventory", "Pocket Inventory", []string{scopeItemsRead, scopeItemsWrite, scopeItemsAdjust}, "Your pantry: groceries as Inventory items"}
	appSplit     = pocketApp{"split", "Pocket Split", []string{scopeGroupsRead, scopeExpensesWrite}, "Shopping costs as expenses in a shared group"}
	appHabits    = pocketApp{"habits", "Pocket Habits", []string{scopeHabitsRead, scopeEntriesWrite}, "Tick off a habit when you cook"}
	pocketApps   = []pocketApp{appInventory, appSplit, appHabits}
)

// linkUses is the manifest's Uses.
func linkUses() []core.Use {
	out := make([]core.Use, 0, len(pocketApps))
	for _, a := range pocketApps {
		out = append(out, core.Use{App: a.slug, Scopes: a.scopes, Reason: a.reason})
	}
	return out
}

// Connection modes, chosen per person and per app in Settings.
const (
	modeCore   = "core"
	modeDirect = "direct"
	modeOff    = "off"
)

// connMode is the mode in effect: the chosen one, else direct when a token
// was entered (how it always worked), else Pocket Core when Kitchen is
// connected to one, else direct (waiting for a URL and token).
func (s *Server) connMode(mode, token string) string {
	switch mode {
	case modeCore, modeDirect, modeOff:
		return mode
	}
	if token != "" || !s.core.Configured() {
		return modeDirect
	}
	return modeCore
}

// notConnected means the person has no working connection to the app
// (switched off, nothing entered, or the Core link is off): Kitchen then
// works on its own, as it always did without the integration.
type notConnected struct{ msg string }

func (e *notConnected) Error() string { return e.msg }

func isNotConnected(err error) bool {
	var nc *notConnected
	return errors.As(err, &nc)
}

// siblingConn is how Kitchen reaches one person's Inventory, Split or Habits.
type siblingConn struct {
	client *pocket.Client
	via    string // direct | core
	public string // what a browser opens
}

func linkReason(app, reason string) string {
	switch reason {
	case "off_person":
		return "you switched the Kitchen → " + app + " link off in Pocket Core (Settings → Apps acting for me)"
	case "off_admin":
		return "the Kitchen → " + app + " link is switched off in Pocket Core (Links)"
	case "target_missing":
		return app + " is not registered with Pocket Core"
	case "not_offered":
		return "this " + app + " version does not accept app links yet"
	case "not_declared":
		return "Pocket Core has not seen this Kitchen version yet; it re-registers within a minute of starting"
	case "not_in_audience":
		return "you do not have access to " + app + " in Pocket Core"
	case "unknown_user":
		return "your email is not a person in Pocket Core"
	}
	return ""
}

// connSettings returns a person's saved mode, URL, token and public URL for an app.
func connSettings(st store.Settings, a pocketApp) (mode, url, token, public string) {
	switch a.slug {
	case "inventory":
		return st.InventoryMode, st.InventoryURL, st.InventoryToken, ""
	case "split":
		return st.SplitMode, st.SplitURL, st.SplitToken, st.SplitPublicURL
	case "habits":
		return st.HabitsMode, st.HabitsURL, st.HabitsToken, ""
	}
	return "", "", "", ""
}

// siblingFor connects to app a for person u with exactly these scopes.
// A *notConnected error means "not connected" (work locally); anything else
// is a real failure (Core or the app unreachable).
func (s *Server) siblingFor(ctx context.Context, u *store.User, st store.Settings, a pocketApp, scopes ...string) (*siblingConn, error) {
	if u == nil {
		return nil, &notConnected{"nobody to act for"}
	}
	mode, url, token, public := connSettings(st, a)
	switch s.connMode(mode, token) {
	case modeOff:
		return nil, &notConnected{a.name + " is switched off in Settings → Integrations"}
	case modeDirect:
		if token == "" {
			return nil, &notConnected{a.name + " is not set up (Settings → Integrations): add a URL and token, or switch to Pocket Core"}
		}
		base := url
		if base == "" {
			base = s.resolveSibling(ctx, a.slug)
		}
		if base == "" {
			return nil, &notConnected{"set the " + a.name + " URL in Settings → Integrations"}
		}
		return &siblingConn{client: pocket.New(base, token, 15*time.Second), via: modeDirect, public: firstNonEmpty(public, base)}, nil
	}
	if !s.core.Configured() {
		return nil, &notConnected{a.name + " through Pocket Core: there is no Pocket Core on this box; use a direct connection"}
	}
	tok, err := s.core.ActAs(ctx, u.Email, a.slug, scopes...)
	if err != nil {
		var le *core.LinkError
		if errors.As(err, &le) {
			if t := linkReason(a.name, le.Reason); t != "" {
				return nil, &notConnected{a.name + " is not set up: " + t}
			}
			return nil, &notConnected{a.name + " is not set up: " + le.Error()}
		}
		return nil, fmt.Errorf("%s through Pocket Core: %w", a.name, err)
	}
	if tok.URL == "" || tok.AccessToken == "" {
		return nil, fmt.Errorf("%s through Pocket Core: Core gave no address or token", a.name)
	}
	return &siblingConn{client: pocket.New(tok.URL, tok.AccessToken, 15*time.Second), via: modeCore, public: firstNonEmpty(public, firstNonEmpty(tok.PublicURL, tok.URL))}, nil
}

// connStatus says, without issuing anything, how a person reaches an app:
// the mode in effect, whether it works ("core" / "direct", "" for not
// connected) and why not (or which scopes are off when only some are).
func (s *Server) connStatus(ctx context.Context, u *store.User, st store.Settings, a pocketApp) (eff, via, detail string) {
	mode, _, token, _ := connSettings(st, a)
	eff = s.connMode(mode, token)
	switch eff {
	case modeOff:
		return eff, "", "switched off"
	case modeDirect:
		if token == "" {
			return eff, "", "no token yet"
		}
		return eff, modeDirect, ""
	}
	if u == nil || !s.core.Configured() {
		return eff, "", "there is no Pocket Core on this box"
	}
	list, err := s.core.LinkStatus(ctx, u.Email, a.slug)
	if err != nil {
		return eff, "", "Pocket Core did not answer: " + err.Error()
	}
	state := map[string]core.LinkState{}
	for _, l := range list {
		state[l.Scope] = l
	}
	var on, off []string
	reason := ""
	for _, sc := range a.scopes {
		l, ok := state[sc]
		if ok && l.On {
			on = append(on, sc)
			continue
		}
		off = append(off, sc)
		if reason == "" {
			reason = "not_declared"
			if ok {
				reason = l.State
			}
		}
	}
	text := linkReason(a.name, reason)
	if text == "" {
		text = reason
	}
	switch {
	case len(off) == 0:
		return eff, modeCore, ""
	case len(on) == 0:
		return eff, "", text
	}
	sort.Strings(off)
	return eff, modeCore, "partly: " + strings.Join(off, ", ") + " off (" + text + ")"
}

// ---- per-request connections ----

// clients carries one person's settings and their connections for the work
// of a single request (or one background run). Connections are made when
// first needed, with the scopes asked for, and remembered only until the
// request ends, so a cook deducting ten ingredients asks Core once.
type clients struct {
	s      *Server
	u      *store.User
	st     store.Settings
	notify *notify.Client
	// noInventory keeps this request local (a pantry row added "here only").
	noInventory bool
	conns       map[string]connResult
}

type connResult struct {
	c   *siblingConn
	err error
}

// clients builds the connections from ONE user's own settings: a member who
// has not connected Inventory / Split / Habits (and has no Core link) gets no
// connection, never the owner's tokens.
func (s *Server) clients(ctx context.Context, uid int64) *clients {
	st, _ := s.store.GetSettings(ctx, uid)
	u := userFrom(ctx)
	if u == nil || u.ID != uid {
		u, _ = s.store.GetUser(ctx, uid)
	}
	return &clients{s: s, u: u, st: st, notify: notify.New(st.NotifyURL, st.NotifyFormat), conns: map[string]connResult{}}
}

func (c *clients) conn(ctx context.Context, a pocketApp, scopes ...string) (*siblingConn, error) {
	if a.slug == appInventory.slug && c.noInventory {
		return nil, &notConnected{"kept in Kitchen only"}
	}
	sc := append([]string(nil), scopes...)
	sort.Strings(sc)
	key := a.slug + " " + strings.Join(sc, " ")
	if r, ok := c.conns[key]; ok {
		return r.c, r.err
	}
	sib, err := c.s.siblingFor(ctx, c.u, c.st, a, scopes...)
	c.conns[key] = connResult{sib, err}
	return sib, err
}

// optional turns "not connected" into (nil, nil): the caller then works
// locally, as without the integration. Real failures stay errors.
func optional(sib *siblingConn, err error) (*siblingConn, error) {
	if isNotConnected(err) {
		return nil, nil
	}
	return sib, err
}

// inventory is optional everywhere: without a connection the pantry simply
// lives in Kitchen. Split and Habits return "not connected" as an error so
// the caller can say why nothing was posted or ticked.
func (c *clients) inventory(ctx context.Context, scopes ...string) (*siblingConn, error) {
	return optional(c.conn(ctx, appInventory, scopes...))
}

func (c *clients) split(ctx context.Context, scopes ...string) (*siblingConn, error) {
	return c.conn(ctx, appSplit, scopes...)
}

func (c *clients) habits(ctx context.Context, scopes ...string) (*siblingConn, error) {
	return c.conn(ctx, appHabits, scopes...)
}
