// Package admin is the shell: the pages a person sees, as opposed to the routes
// a program calls.
//
// It writes almost no screens. ARCHITECTURE.md's eighth idea is that list,
// detail and form come from an entity's schema, and kit/httpx now carries that
// schema for every resource kit/rest mounted — so this module asks the API what
// exists and generates seven pages for each answer. What is written by hand is
// the frame around them, the dashboard, the health page, the sign-in page and
// the gallery: five screens for the whole application, and a sixth arrives only
// when an interaction cannot be derived.
//
// It is composed last, and that is load-bearing rather than tidy: kit/app calls
// each module's Routes in composition order, so a module mounted after this one
// registers a resource whose screens were already generated — which is to say,
// were not.
package admin

import (
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/admin/internal"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
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

	// Tenants is the control plane, for the switcher. It is the one cross-tenant
	// read in this module, and it needs the token Routes is handed.
	Tenants tenantcontracts.Service
}

// Module is the manifest.
//
// It declares no permissions and no events: every page is guarded by a
// permission the module that owns the data defined, which is the point — a
// screen that could be reached by somebody the API would refuse is a screen
// that leaks. It declares no nav entry either, because the shell is not a
// destination.
func Module(deps Deps) module.Module {
	return module.Module{
		Name:          "admin",
		Permissions:   nil,
		Events:        nil,
		Nav:           nil,
		Jobs:          nil,
		Subscriptions: nil,
		Routes: func(api *httpx.API) {
			internal.Mount(api, internal.Shell{
				Nav:       navigation(deps.Modules),
				Checks:    checks(deps.Modules),
				Authorize: deps.Authorize,
				Tenants:   deps.Tenants,
				// The one call in this module that crosses a tenant boundary,
				// in the manifest a reviewer is already reading. It is what the
				// tenant switcher lists. See docs/adr/0006.
				Token: api.SystemToken(),
			})
		},
	}
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

// checks is every module's health checks, for the health page. kit/app collects
// the same list for /ready; this one is rendered rather than counted, so a
// person can see which dependency is the unhappy one.
func checks(mods []module.Module) []health.Check {
	var out []health.Check
	for _, m := range mods {
		out = append(out, m.Health...)
	}
	return out
}
