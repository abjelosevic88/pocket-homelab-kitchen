package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core), Kitchen as
// the target: another Pocket app the person allowed in Core (Pocket Day, Pocket Nutrition) may
// read here for them with a pcx_ token, only within these scopes. Every route
// not listed in linkRoutes stays closed to it (403).

var linkOffers = []core.Offer{
	{Scope: "cooklog:read", Name: "See what you cooked", Description: "Read your cook log (what you cooked and when), for your journal."},
	{Scope: "recipes:read", Name: "See your recipes", Description: "Read your recipes with their ingredients and nutrition per serving, so a food diary can log one you cooked. Nothing can be changed with it."},
	{Scope: "plan:read", Name: "See your meal plan", Description: "Read the meals you planned for each day, so a food diary can show them and log one in a tap. Nothing can be changed with it."},
}

// portable is what Pocket Move may copy out and bring in for a person
// (docs/move.md in pocket-homelab-core); Register adds move:export/import.
var portable = &core.Portable{
	Exports: []core.PortableExport{
		{Path: "/api/v1/export.json", File: "kitchen.json", Kind: core.KindPocket, Title: "Your recipes, meal plan, shopping lists, pantry, what you cooked and your shopping trips"},
	},
	Imports: []core.PortableImport{
		{Path: "/api/v1/import", Kind: core.KindPocket, ContentType: "application/json", MaxBytes: 32 << 20, Title: "Adds the recipes, plan, lists, pantry and cook log from another box; recipes with the same title, and what is already there, are left alone"},
	},
}

type linkRoute struct {
	method string
	path   *regexp.Regexp
	scope  string // "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/cooklog$`), "cooklog:read"},
	{"GET", regexp.MustCompile(`^/api/v1/recipes$`), "recipes:read"},
	{"GET", regexp.MustCompile(`^/api/v1/recipes/\d+$`), "recipes:read"},
	{"GET", regexp.MustCompile(`^/api/v1/plan$`), "plan:read"},
}

var errLinkScope = errors.New("outside the link's scope")

// linkAllows reports whether a link token with these scopes may make this request.
func linkAllows(scopes []string, r *http.Request) bool {
	if portable.LinkAllows(scopes, r) {
		return true
	}
	for _, lr := range linkRoutes {
		if lr.method != r.Method || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token, checks the route against its scopes and
// maps it to the person it acts for (their own kitchen only).
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, err
	}
	if !in.Active || !in.Delegated() || in.User == nil || in.User.Email == "" {
		return nil, http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, errLinkScope
	}
	return s.coreIdentityUser(ctx, in)
}
