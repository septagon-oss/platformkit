package httpx

import (
	"fmt"
	"strings"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Router is one surface of one module. The two mount doors are Register and
// HTML — the two this package already had, because which of them a caller uses
// is what decides whether the route is a document or a value, which is the
// distinction the two prefixes encode. A verb-named door (r.App.Get) would need
// its own operation struct per verb, could not carry huma.Operation (its id,
// summary, tag and errors), and would be a third registration path beside those
// two; a Go method cannot take type parameters, so the doors are functions
// taking the router.
type Router struct {
	screenOwner string
	surface     Surface
	module      string
	api         *API
	// atRoot says this router came from Home and therefore mounts at the
	// surface's own root rather than beneath the module's segment.
	atRoot bool
	// at is a declared protocol address (see At): the whole prefix, so a route
	// mounted here composes <at>/… and takes no part of the surface table.
	at string
}

// Surface is which of the three this router is.
func (r *Router) Surface() Surface { return r.surface }

// Module is the manifest name this router mounts beneath.
func (r *Router) Module() string { return r.module }

// Prefix is the JSON prefix a route mounted here takes: the composed
// /api/v1/… address, surface and module included.
func (r *Router) Prefix() string { return r.jsonPrefix() }

// Path is the address a route mounted at this router with Register would take:
// the /api/v1/… one. It is the same composition the mount performs, so a shell
// can link, or a test can call, an address it did not register itself — writing
// the answer out by hand is how a module comes to hard-code a prefix.
func (r *Router) Path(rel string) string { return r.compose(rel, false) }

// PagePath is the address the document mounted at rel answers at — the page a
// browser navigates to, which a surface that serves no documents has no answer
// for. Use it to link a page you did not mount yourself; use Path for a route.
func (r *Router) PagePath(rel string) string {
	if r.surface == SurfaceOps {
		r.api.noteRefusal(fmt.Sprintf("httpx: the %s router was asked for the page %q; the control plane serves no documents — its chrome is the workspace at the installation host", r.surface, rel))
		return ""
	}
	return r.compose(rel, true)
}

// ForModule returns this surface's router bound to another module's namespace:
// the workspace screens of the task resource belong at /app/task/tasks whoever
// generated them. It is the door the generated shell uses, and it is bounded by
// the composition: a name that was never composed refuses at once, so a typo
// cannot quietly invent a namespace.
func (r *Router) ForModule(module string) *Router {
	r.api.mu.Lock()
	known := r.api.known[module]
	r.api.mu.Unlock()
	if !known {
		panic(fmt.Sprintf("httpx: %s router asked for module %q, which this composition never registered; a route lives in a module that exists", r.surface, module))
	}
	return &Router{surface: r.surface, module: module, screenOwner: r.screenOwner, api: r.api}
}

// Known reports whether the composition registered a module of that name. The
// generated shell asks it before it mounts a page into another module's
// namespace: the tenant switcher belongs at /app/tenant/tenants because that is
// where the workspace screens of the tenant module live, and a composition
// without the tenant module has no such namespace to mount into.
func (r *Router) Known(module string) bool {
	r.api.mu.Lock()
	defer r.api.mu.Unlock()
	return r.api.known[module]
}

// Home claims the surface's root for this module: routes mounted on the
// returned router land at "/" and not at "/<surface>/<module>". The boolean
// says whether this call *took* the claim — one claimant per composition, and
// the first in composition order gets it. A module that answers false may still
// mount on the returned router at its own paths; mounting at the claimed root
// itself is refused at compose (see the A4 message).
//
// The JSON prefix is not part of the claim: two modules' JSON routes must not
// collide, so a home router still composes /api/v1/<module>/… for its value
// routes. The claim is about who answers the visitor at the root of the site or
// the workspace, which is one address and can have one owner.
func (r *Router) Home() (*Router, bool) {
	home := &Router{surface: r.surface, module: r.module, screenOwner: r.screenOwner, api: r.api, atRoot: true}
	if r.surface == SurfaceOps {
		// The control plane has no root to serve: its own address is a 404 at
		// every host but the installation's, and a page is refused there
		// anyway. Recorded for the compose gate rather than panicked, because
		// the caller wrote a legal-looking line in a manifest.
		r.api.noteRefusal(fmt.Sprintf("httpx: module %q: the Ops surface has no home to claim — claim the workspace home with r.App.Home", r.module))
		return r, false
	}
	return home, r.api.claim(r.surface, r.module)
}

// SystemToken is the capability API.SystemToken hands to a module at the one
// moment it is being wired. It is forwarded here because the value a module's
// Routes receives is now the only thing it holds.
func (r *Router) SystemToken() tenancy.SystemToken { return r.api.SystemToken() }

// InvalidateHost forgets cached host resolutions, so a rename, a suspension or a
// new language declaration takes effect now rather than within the cache's own
// lifetime. The tenant module calls it; nothing else has a host to forget.
//
// However many hosts it names, the call is one Move of the host namespace, because
// an invalidation that only deletes what is there loses the load that raced it. It
// returns the store's error: unwinding the committed write that precedes it is the
// worse outage, and so is answering as though every replica had already forgotten.
func (r *Router) InvalidateHost(hosts ...string) error { return r.api.InvalidateHost(hosts...) }

// The two prefixes of the table. A surface with no document prefix refuses
// HTML at the door (R6), which is why the table has a hole in it rather than a
// fourth guess.
func (r *Router) jsonPrefix() string {
	// A declared address is the whole prefix: the table composes nothing onto
	// it, which is the point — the protocol named this address, not the table.
	if r.at != "" {
		return r.at
	}
	// A router with no module name is the composition's own, and the
	// composition's namespace is the surface's segment: /api/v1/app, the
	// address the workspace catalog answers at. A module never gets one of
	// these — module.Validate refuses the names — so nothing else can claim it.
	if r.module == "" {
		switch r.surface {
		case SurfacePublic:
			return join(apiRoot, publicJSONSegment)
		case SurfaceOps:
			return join(apiRoot, opsJSONSegment)
		}
		return join(apiRoot, "app")
	}
	switch r.surface {
	case SurfacePublic:
		return join(apiRoot, publicJSONSegment, r.module)
	case SurfaceOps:
		return join(apiRoot, opsJSONSegment, r.module)
	}
	return join(apiRoot, r.module)
}

func (r *Router) pagePrefix() string {
	switch r.surface {
	case SurfacePublic:
		if r.atRoot || r.module == "" {
			return ""
		}
		return "/" + r.module
	case SurfaceOps:
		return ""
	}
	if r.atRoot || r.module == "" {
		return AppRoot
	}
	return join(AppRoot, r.module)
}

// join is the one way a prefix is assembled, so that a router with no module
// name — the composition's own, which serves the workspace catalog — composes
// one slash and not two.
func join(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.Trim(p, "/"); p != "" {
			out = append(out, p)
		}
	}
	return "/" + strings.Join(out, "/")
}

// compose is the mount address of a relative path: the prefix and the path, and
// nothing else. It is one function so that the two doors, Path, the shell's
// links and the gate all mean the same address.
func (r *Router) compose(rel string, page bool) string {
	if fault := r.relFault(rel); fault != "" {
		r.api.noteRefusal(fault)
		// Compose it anyway: the process never listens, because the gate that
		// reads the refusal runs before anything opens a port.
	}
	prefix := r.jsonPrefix()
	if page {
		prefix = r.pagePrefix()
	}
	// Either spelling composes to the same address. A route declared without its
	// leading slash is refused at Mount, where the recording stops the process
	// from listening; this is a question, and a question that answered a
	// half-joined address would send the asker somewhere that 404s.
	if !strings.HasPrefix(rel, "/") {
		rel = "/" + rel
	}
	at := prefix + strings.TrimSuffix(rel, "/")
	if at == "" {
		// The surface's own root, asked for by the module that claimed it: the
		// site's home page is at "/", which is the whole reason the claim exists.
		// An empty address is not a route, and huma would say so unhelpfully.
		return "/"
	}
	return at
}

// relFault is R4: a relative path that names a prefix, a surface, or its own
// module. The answer it names is what the caller meant to write.
func (r *Router) relFault(rel string) string {
	const shape = "a route is relative to its module and its surface"
	if !strings.HasPrefix(rel, "/") {
		return fmt.Sprintf("httpx: the %s router of module %q: %q is not a path; %s — write %q", r.surface, r.module, rel, shape, "/"+rel)
	}
	prefixes := []string{apiRoot, AppRoot, OpsRoot, "/admin", "/public"}
	// A router that claimed the surface's root composes no namespace at all, so
	// writing its own module name in a path is not the doubling this refuses — it
	// is how the claimant keeps its asset tree out of the root it was handed
	// (/web/assets rather than /assets, which every later claimant would want).
	if r.module != "" && !r.atRoot {
		prefixes = append(prefixes, "/"+r.module)
	}
	// A declared protocol address is a prefix of the same kind: a route that
	// reaches it by writing it out relative to some other router would answer at
	// two addresses, and the gate that refuses a doubled /api/v1/api/v1 refuses
	// a doubled /scim/v2/scim/v2 for the same reason.
	prefixes = append(prefixes, r.api.declaredPrefixes()...)
	for _, taken := range prefixes {
		if rel == taken || strings.HasPrefix(rel, taken+"/") {
			want := "/" + strings.TrimPrefix(strings.TrimPrefix(rel, taken), "/")
			return fmt.Sprintf("httpx: the %s router of module %q: %q names a prefix; %s — write %q", r.surface, r.module, rel, shape, want)
		}
	}
	return ""
}
