// Package admin is the shell: the pages a person sees, as opposed to the routes
// a program calls.
//
// It writes almost no screens. ARCHITECTURE.md's eighth idea is that list,
// detail and form come from an entity's schema, and kit/httpx carries that
// schema for every resource kit/rest mounted. The generated screens are
// ui/screens'; this module composes them with its own chrome, frame and
// navigation, adds the pages no schema describes — the dashboard, health,
// sign-in, gallery, tenant switcher and delivery inspection — and
// serves the same knowledge as JSON at /api/v1/admin/resources for a shell that
// is not a browser. A hand-written page arrives only when an interaction
// cannot be derived.
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
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/page"
)

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

	// Tenants is the control plane, for the switcher. Its cross-tenant read
	// needs the system token Routes is handed, as does delivery inspection.
	Tenants tenantcontracts.Service

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
	Storybook func(context.Context) (ui.Storybook, error)
}

const PermissionGalleryRead = "gallery:read"

// PermissionDeliveryRead permits installation-wide delivery metadata inspection.
const PermissionDeliveryRead = internal.PermissionDeliveryRead

// Module is the manifest.
//
// It declares gallery access and operator delivery inspection. Other pages use
// the permissions of their data owners. Inspection emits no event or command.
func Module(deps Deps) module.Module {
	delivery := internal.DeliveryGrant()
	nav := []module.NavEntry{{Label: "Event delivery", Path: internal.DeliveryPath, Permission: delivery.Permission}}
	return module.Module{
		Name:          "admin",
		Permissions:   []module.Permission{{Key: PermissionGalleryRead}, {Key: delivery.Permission, Operator: delivery.Operator}},
		Events:        nil,
		Nav:           nav,
		Jobs:          nil,
		Subscriptions: nil,
		Routes: func(api *httpx.API) {
			internal.Mount(api, internal.Shell{
				Nav:       append(navigation(deps.Modules), nav...),
				Authorize: deps.Authorize,
				Tenants:   deps.Tenants,
				Theme:     theme(deps.Theme),
				Storybook: deps.Storybook,
				Messages:  deps.Messages,
				Locale:    deps.Locale,
				// Tenant listing and delivery inspection explicitly read across
				// tenants, using operator-protected routes. See docs/adr/0006.
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
