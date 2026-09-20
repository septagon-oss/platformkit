// Package admin is the shell: the pages a person sees, as opposed to the routes
// a program calls.
//
// It writes almost no screens. ARCHITECTURE.md's eighth idea is that list,
// detail and form come from an entity's schema, and kit/httpx carries that
// schema for every resource kit/rest mounted. The generated screens are
// ui/screens'; this module composes them with its own chrome, frame and
// navigation, adds the six pages no schema describes — the dashboard, the
// health page, the sign-in page, the gallery, the tenant switcher and the roles
// screen — and serves the same knowledge as JSON at /api/v1/admin/resources for
// a shell that is not a browser. A seventh hand-written page arrives only when
// an interaction cannot be derived, which is what the roles screen is: a role is
// keyed by its name rather than by an id, and a generated screen's item path is
// a UUID.
//
// It is composed last, and that is load-bearing rather than tidy: kit/app calls
// each module's Routes in composition order, so a module mounted after this one
// registers a resource whose screens were already generated — which is to say,
// were not.
package admin

import (
	"context"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/admin/internal"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/ui/export"
	"github.com/septagon-oss/platformkit/ui/page"
)

// Roles is what the shell needs of the auth module to administer roles. See
// internal.Roles: the alias keeps the declaration beside its one implementation
// and the name beside the Deps field that takes it.
type Roles = internal.Roles

// Deps is what the shell cannot make for itself.
type Deps struct {
	// Modules is the composition, for navigation. The shell draws the sidebar
	// from every module's Nav, so it needs the list main built; a copy
	// maintained here would be a second answer to "what is in this
	// application", and the one that goes stale.
	Modules []module.Module

	// Authorize is the same value the kernel enforces with. The sidebar shows a
	// link only when the caller may follow it, and asking the authorizer is
	// what makes that the same answer the route would give — a nav filter with
	// rules of its own is a menu that lies in one direction or the other.
	Authorize httpx.Authorizer

	// Tenants is the control plane, for the switcher. It is the one cross-tenant
	// read in this module, and it needs the token Routes is handed.
	Tenants tenantcontracts.Service

	// Roles is the auth module's role administration, for the screen that
	// module's nav entry names. It is the same value main hands the kernel as
	// the authorizer, seen through the two methods this screen uses.
	//
	// Nil mounts no screen. A composition that has no auth module has no roles
	// nav entry either, so the two agree; a composition that has one and wires
	// nothing here is told at boot that the entry leads nowhere.
	Roles Roles

	// Theme is the installation's two palettes. The zero value is the palette
	// this repository ships; a client with its own colours sets this and
	// changes nothing else, because every rule above the tokens is written in
	// terms of a role. See design.Pair.
	Theme design.Pair
	// Messages and Locale opt the sign-in page into translated copy. Compose
	// Messages() or an application catalog before mounting. Other pages keep
	// their authored language. Locale selects a preference before the browser.
	Messages page.Messages
	Locale   func(context.Context, page.Request) string

	// Storybook selects and authorizes the composition for the resolved tenant
	// and principal in ctx. Return an error to deny access; never select from a
	// query parameter or fall back to another tenant. Nil exposes Core only to
	// the operator tenant. Empty Examples stays empty. Every gallery endpoint
	// also requires PermissionGalleryRead before calling this function.
	Storybook func(context.Context) (export.Storybook, error)
}

const PermissionGalleryRead = "gallery:read"

// Module is the manifest.
//
// It declares gallery:read for the selected design composition. Other pages
// use the permissions of the modules that own their data — the tenant
// switcher's and the roles screen's included, which is what keeps a screen and
// its module's API refusing the same callers. It declares neither events nor a
// navigation entry of its own.
func Module(deps Deps) module.Module {
	return module.Module{
		Name:          "admin",
		Permissions:   []module.Permission{{Key: PermissionGalleryRead}},
		Events:        nil,
		Nav:           nil,
		Jobs:          nil,
		Subscriptions: nil,
		Routes: func(api *httpx.API) {
			internal.Mount(api, internal.Shell{
				Nav:       navigation(deps.Modules),
				Authorize: deps.Authorize,
				Tenants:   deps.Tenants,
				Roles:     deps.Roles,
				Theme:     theme(deps.Theme),
				Storybook: deps.Storybook,
				Messages:  deps.Messages,
				Locale:    deps.Locale,
				// The one call in this module that crosses a tenant boundary,
				// in the manifest a reviewer is already reading. It is what the
				// tenant switcher lists. See docs/adr/0006.
				Token: api.SystemToken(),
			})
		},
	}
}

// theme is the palette this shell serves: the caller's, or the one this
// repository ships when they said nothing. A zero Pair is "no opinion" rather
// than "no colours", so an application that never mentions design still has a
// stylesheet.
func theme(chosen design.Pair) design.Pair {
	if chosen == (design.Pair{}) {
		return design.Default()
	}
	return chosen
}

// navigation is every module's nav entries, in composition order, which is the
// order main lists the modules in. There is no second ordering: see
// module.NavEntry.
func navigation(mods []module.Module) []module.NavEntry {
	var out []module.NavEntry
	for _, m := range mods {
		out = append(out, m.Nav...)
	}
	return out
}
