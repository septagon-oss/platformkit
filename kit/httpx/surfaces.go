package httpx

// surfaces.go is the answer to a question this package used to ask every route
// separately: whose page is this, and who may be standing in front of it?
//
// A module now names a *router* — `r.App`, `r.Public`, `r.Ops` — and a path
// relative to it. The prefix, the module segment and the surface are composed
// here, from the manifest name and the router, which is what makes three
// things true at once: a module cannot write a prefix (there is no string to
// write it in), a route cannot be on two surfaces (the value is a sum type, not
// three booleans), and the middleware that behaves differently per surface and
// the gate that refuses a contradiction read the same recorded answer.
//
// # The prefix table
//
// Surface  JSON                       document               served at
// Public   /api/v1/public/<module>    /<module>               every host
// App      /api/v1/<module>           /app/<module>           every host
// Ops      /api/v1/ops/<module>       none — see R6          Installation.Host only
//
// A module that takes the surface's Home claims its root instead: the Public
// home is `/` and `/{slug}`, the App home is `/app` and `/app/{x}`. See Home.
//
// # Why one router and not three
//
// Chi refuses a middleware added after the first route, and huma mounts one
// API's operations on one mux, so three muxes at three prefixes would leave a
// surface's *other* prefix — the page beside the JSON — nowhere to live, and
// would take the mount-time refusals away with it. So there is one mux, one API
// and one recorded table, and classify is the one pure function that says which
// chain a path belongs to. The mount gate and the request chain call it, which
// is the loop that keeps them from drifting apart.

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Surface is which of the three a route is on. It is a type rather than three
// booleans because "public and operator" is a contradiction, and because the
// closed set is what the middleware chains are built from.
type Surface string

const (
	// SurfacePublic is the tenant's face to a visitor: no session is parsed,
	// no cookie is ever set, and a response is cacheable and indexable.
	SurfacePublic Surface = "public"
	// SurfaceApp is the workspace: deny by default, no-store, noindex, the
	// session cookie, the strictest policy of the three.
	SurfaceApp Surface = "app"
	// SurfaceOps is the installation's control plane, served at the
	// installation host and nowhere else, and JSON only.
	SurfaceOps Surface = "ops"
)

// The prefixes of the table, and the two roots a surface answers at when
// nothing is mounted beneath it.
const (
	apiRoot = "/api/v1"
	// AppRoot is the workspace's own address: the home of the generated shell.
	AppRoot = "/app"
	// OpsRoot is the control plane's own address, served at the installation
	// host only.
	OpsRoot           = "/ops"
	catalogPath       = apiRoot + "/app/resources"
	publicJSONSegment = "public"
)

// classify is the whole of "which surface is this request on". It reads the
// path and nothing else, so it can run before routing — which is what lets the
// Ops host gate answer a 404 ahead of the router, and what lets the mount-time
// refusals and the request-time chains read the same answer.
//
// The two fallbacks are deliberate and are not symmetric, so the reason for
// each is here:
//
//   - A path under /api/v1/ that names no surface belongs to the workspace.
//     Public JSON lives under /api/v1/public/ by construction, so the only
//     thing left beneath the API root is a module's own route; a request at an
//     address nothing mounted gets the stricter chain, and a stricter chain
//     that answers 404 costs nothing.
//   - Anything else belongs to the public face. A page lives at an address
//     nobody can predict from its prefix — `/`, `/pricing`, `/news/2` — so the
//     fallback has to be Public, and Public is the one surface where guessing
//     is safe: it parses no session, sets no cookie, and can hold no route that
//     needs a principal, because R1 refuses those at mount. A route that lands
//     here by accident loses powers rather than gaining them.
func classify(path string) Surface {
	switch {
	case path == OpsRoot || strings.HasPrefix(path, OpsRoot+"/") ||
		strings.HasPrefix(path, apiRoot+"/ops/"):
		return SurfaceOps
	case path == AppRoot || strings.HasPrefix(path, AppRoot+"/") ||
		strings.HasPrefix(path, apiRoot+"/app/"):
		return SurfaceApp
	case strings.HasPrefix(path, apiRoot+"/public/"):
		return SurfacePublic
	case strings.HasPrefix(path, apiRoot+"/"):
		return SurfaceApp
	}
	return SurfacePublic
}

// Workspace is the address a navigation entry names. An entry says
// "task/tasks" — the module and the entity, relative to the workspace — and
// this is the one place the prefix is added, because ui/page must be able to
// resolve an entry without being told where the shell lives.
func Workspace(screen string) string { return AppRoot + "/" + strings.TrimPrefix(screen, "/") }

// SurfaceExtension is the OpenAPI extension naming the surface a route was
// mounted on. Like x-platformkit-auth it is a document key as well as a
// runtime one: the surface a projection reads is the surface the chain applies,
// because the kernel wrote both from the one value at mount. Nothing in this
// package reads it back; ValidateDeclarations checks the composed path
// classifies to it, and a projection or a test reads the document.
const SurfaceExtension = "x-platformkit-surface"

// Surfaces is what a module's Routes receives: the three routers the kernel
// built, each already bound to this module's name. A module writes a path
// relative to the router it chose and nothing else.
//
// The composition-level questions are forwarded as methods rather than by
// handing out the *API: a module that held the API could mount through it and
// skip its router, which is the one door this type exists to close.
type Surfaces struct {
	// Public is the tenant's face to an anonymous visitor.
	Public *Router
	// App is the workspace: the generated shell and the module's own JSON.
	App *Router
	// Ops is the installation's control plane. A module mounts here what the
	// installation owns — see httpx.OperatorPermission — and the surface is
	// served at the installation host only.
	Ops *Router

	api *API
}

// Surfaces builds the three routers of one module. kit/app calls it once per
// composed module, in composition order, and hands the result to that module's
// Routes; nothing else can, because the one place the whole module list exists
// is the composition.
//
// The empty module names the kernel's own routes: the catalog the shell reads
// lives at /api/v1/app/resources, an address no module composes because it
// belongs to the composition rather than to a capability. A module never passes
// "": it is handed its routers, and only kit/app asks for this one.
func (a *API) Surfaces(module string) Surfaces {
	a.mu.Lock()
	if a.known == nil {
		a.known = map[string]bool{}
	}
	a.known[module] = true
	a.mu.Unlock()
	s := Surfaces{api: a}
	s.Public = &Router{surface: SurfacePublic, module: module, api: a}
	s.App = &Router{surface: SurfaceApp, module: module, api: a}
	s.Ops = &Router{surface: SurfaceOps, module: module, api: a}
	return s
}

// Resources, Permissions, Required, Recorded, Mounted, RegisterResource and
// AddCommand forward the composition-level questions a module asks the kernel
// while it is being wired. They are methods on the value it already holds so
// that the value is the whole of the seam: nothing a module legitimately needs
// requires the *API.

func (s Surfaces) Resources() []Resource        { return s.api.Resources() }
func (s Surfaces) Permissions() []tenancy.Grant { return s.api.Permissions() }
func (s Surfaces) Required() []tenancy.Grant    { return s.api.Required() }
func (s Surfaces) Recorded() []*huma.Operation  { return s.api.Recorded() }
func (s Surfaces) Mounted() []MountedRoute      { return s.api.Mounted() }
func (s Surfaces) RegisterResource(r Resource)  { s.api.RegisterResource(r) }
func (s Surfaces) AddCommand(module, entity string, c Command) {
	s.api.AddCommand(module, entity, c)
}

// Router is one surface of one module. The two mount doors are Register and
// HTML — the two this package already had, because which of them a caller uses
// is what decides whether the route is a document or a value, which is the
// distinction the two prefixes encode. A verb-named door (r.App.Get) would need
// its own operation struct per verb, could not carry huma.Operation (its id,
// summary, tag and errors), and would be a third registration path beside those
// two; a Go method cannot take type parameters, so the doors are functions
// taking the router.
type Router struct {
	surface Surface
	module  string
	api     *API
	// atRoot says this router came from Home and therefore mounts at the
	// surface's own root rather than beneath the module's segment.
	atRoot bool
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
	return &Router{surface: r.surface, module: module, api: r.api}
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
	home := &Router{surface: r.surface, module: r.module, api: r.api, atRoot: true}
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

// SystemToken is the credential an in-process caller uses to reach a route that
// needs a principal where no HTTP request carries one. It belongs to the
// composition, not to a surface: which router it is presented to is the caller's
// business. See the API's field of the same name.
func (s Surfaces) SystemToken() tenancy.SystemToken { return s.App.SystemToken() }

// Static mounts this surface's file tree at the address the surface composes for
// it, beneath the module's own namespace. A stylesheet has no tenant and no
// transaction, and a large one must not be buffered waiting for a commit that
// will never happen; that is the whole reason it is not a route.
//
// It is still inside the surface's chain: the tree is mounted on the router that
// carries the request id, the surface classification and the headers, so what a
// cache may hold of it is that surface's answer and nothing else — a workspace
// tree is no-store and noindex, a public one is cacheable for its minute. The
// chain covers a miss as well as a hit: a file the tree does not hold is refused
// by the surface and not by net/http, in the shape the client asked for (see
// API.tree). And the tree is recorded in the mount table like every other
// address, so the composition's own audit of what it serves includes the tree it
// serves it from.
//
// Three refusals, all collected rather than panicked, because the composition
// that trips one never listens and whoever wrote it should read all of it at
// once: a tree on the control plane, which serves no documents and would be
// published at every customer's host under a public-looking address; a tree at
// the root of a namespace, where the routes of that namespace answer, because
// chi would then let the file server answer for `/app/<module>/anything`; and
// the rel that names a prefix, which relFault refuses a route for as well.
//
// See API.Probes for the other narrow door, and why neither is a general
// "mount a handler" one.
func (r *Router) Static(rel string, fsys fs.FS) {
	if fault := r.staticFault(rel); fault != "" {
		r.api.noteRefusal(fault)
		// And nothing is mounted: the composition never listens because the gate
		// that reads the refusal runs before anything opens a port, and a tree
		// mounted while it was refused would be a door the audit does not list.
		return
	}
	at := strings.TrimSuffix(r.compose(rel, true), "/")
	r.api.noteMount(mounted{
		surface: r.surface, module: r.module, method: http.MethodGet, path: at + "/*",
		// A file tree asks nothing of anybody, which is the question Public()
		// answers. It is no operation and declares nothing of its own; the boot
		// line prints it beside the workspace doors that admit an anonymous
		// caller, because that is the honest sentence about a stylesheet: whoever
		// reaches the address is served it, with the surface's own headers.
		auth: Public(), page: true,
	})
	r.api.root.Handle(at+"/*", http.StripPrefix(at, r.api.tree(fsys)))
}

// tree is a mounted file tree inside its surface's chain, including where it
// answers nothing.
//
// http.FileServerFS answers a missing file for itself — net/http's plain-text
// "404 page not found" — and that answer is outside the chain the tree was
// mounted into: no problem document for a client that parses one, no failure page
// for the browser that navigated to a stylesheet the deployment no longer ships,
// and no request id to quote. The rule the wrapper applies is therefore the plain
// one — the tree answers where it holds a file, and the surface answers everywhere
// else. That covers the mount prefix and any directory beneath it, which the file
// server would otherwise answer with a listing of the shell's own filenames to
// whoever asked politely.
//
// What the tree does hold goes to the file server untouched — its content type,
// its range support, its conditional response — because the only thing wrong with
// the old answer was the miss.
func (a *API) tree(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Cleaned rather than raw: fs.Stat refuses a path carrying a .., and the
		// refusal that describes a file nobody has is the one for a file that is
		// simply not there.
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if !treeHolds(fsys, name) {
			a.fail(w, r, http.StatusNotFound, "nothing is served at this address")
			return
		}
		files.ServeHTTP(w, r)
	})
}

// treeHolds reports whether this tree has a file at that name. A directory is not
// an address anybody asked for, and the mount prefix is the same case with the name
// empty; no mounted tree here carries an index for either, so refusing them loses
// nothing and forecloses the listing.
func treeHolds(fsys fs.FS, name string) bool {
	if name == "" {
		return false
	}
	info, err := fs.Stat(fsys, name)
	return err == nil && !info.IsDir()
}

// staticFault is the two refusals the prefix rules do not already make: which
// surfaces have documents at all, and who owns a namespace's root.
func (r *Router) staticFault(rel string) string {
	if r.surface == SurfaceOps {
		return fmt.Sprintf("httpx: module %q: Static(%q) on the Ops router would publish a file tree at the document address the control plane does not have; the Ops surface is JSON and is served at the installation host only, and the address it composes for a page is empty, so the tree would answer at every customer's host under a public-looking prefix — mount it on the App router, whose chrome the operator stands in front of", r.module, rel)
	}
	if strings.Trim(rel, "/") == "" {
		return fmt.Sprintf("httpx: module %q: Static(%q) on the %s router mounts a file tree at the root of %s, which is the namespace the routes of %s answer in; a tree is mounted beneath a namespace and not at its root, because chi would let the file server answer for everything beneath it — write Static(\"/assets\", tree)", r.module, rel, r.surface, r.pagePrefix(), r.pagePrefix())
	}
	return ""
}

// SystemToken is the capability API.SystemToken hands to a module at the one
// moment it is being wired. It is forwarded here because the value a module's
// Routes receives is now the only thing it holds.
func (r *Router) SystemToken() tenancy.SystemToken { return r.api.SystemToken() }

// InvalidateHost forgets a cached host resolution, so a rename or a suspension
// takes effect now rather than within the cache's own TTL. The tenant module
// calls it; nothing else has a host to forget.
func (r *Router) InvalidateHost(host string) { r.api.InvalidateHost(host) }

// The two prefixes of the table. A surface with no document prefix refuses
// HTML at the door (R6), which is why the table has a hole in it rather than a
// fourth guess.
func (r *Router) jsonPrefix() string {
	// A router with no module name is the composition's own, and the
	// composition's namespace is the surface's segment: /api/v1/app, the
	// address the workspace catalog answers at. A module never gets one of
	// these — module.Validate refuses the names — so nothing else can claim it.
	if r.module == "" {
		switch r.surface {
		case SurfacePublic:
			return join(apiRoot, publicJSONSegment)
		case SurfaceOps:
			return join(apiRoot, "ops")
		}
		return join(apiRoot, "app")
	}
	switch r.surface {
	case SurfacePublic:
		return join(apiRoot, publicJSONSegment, r.module)
	case SurfaceOps:
		return join(apiRoot, "ops", r.module)
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
	for _, taken := range prefixes {
		if rel == taken || strings.HasPrefix(rel, taken+"/") {
			want := "/" + strings.TrimPrefix(strings.TrimPrefix(rel, taken), "/")
			return fmt.Sprintf("httpx: the %s router of module %q: %q names a prefix; %s — write %q", r.surface, r.module, rel, shape, want)
		}
	}
	return ""
}

// Register mounts an operation on this router, together with the authorization
// it declares, at the address the table gives it. It is the only way a module
// registers a handler.
func Register[I, O any](r *Router, op huma.Operation, auth Auth, handler func(context.Context, *I) (*O, error)) {
	op.Path = r.compose(op.Path, false)
	prepare(r, &op, auth, false)
	huma.Register(r.api.api, op, handler)
}

// prepare is the shared half of the two doors: the panic for the zero Auth, the
// declaration, the surface, and the mount record the compose gate reads. page
// says whether the answer is a document, which is the fact R6 refuses a page on
// the JSON surface with, and the boot line counts.
func prepare(r *Router, op *huma.Operation, auth Auth, page bool) {
	if !auth.Declared() {
		panic("httpx.Register: " + describe(op) + " was passed the zero Auth; use Permission, Public or SignedIn")
	}
	declare(op, auth)
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions[SurfaceExtension] = string(r.surface)
	r.api.noteMount(mounted{surface: r.surface, module: r.module, method: op.Method, path: op.Path, auth: auth, page: page})
}

// SurfaceOf is the surface of the request ctx belongs to, as classify answered
// it on the way in. A renderer asks it to decide what it may say about caching:
// a page with no principal behind it is a page a cache may keep, and only the
// chain knows which surface parsed no session. Outside a request it answers
// Public, which is the surface that grants nothing.
func SurfaceOf(ctx context.Context) Surface {
	s, _ := ctx.Value(surfaceKey{}).(Surface)
	if s == "" {
		return SurfacePublic
	}
	return s
}

type surfaceKey struct{}

// surface is the root middleware that answers the first question about an
// address — which of the three surfaces is this — and puts the answer on the
// request context, where the headers beside it and the session, the authorizer
// and a renderer further down all ask it rather than re-reading the path.
//
// It is its own middleware and not a line inside headers because the answer is
// needed by more than one layer, and the layer that needs it most urgently is
// the one that dresses a refusal the next middleware is about to make: the
// surface must be settled before anything writes a response, so that the
// refusal of an address is the refusal that address gets whatever the answer to
// "is it served here" turns out to be. classify is the whole of the answer; this
// is where it is spent.
func (a *API) surface(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := classify(r.URL.Path)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), surfaceKey{}, s)))
	})
}

// address is the root middleware that answers two questions ahead of routing, in
// one pass, because both are properties of the address rather than of the
// operation, and both have to be answered before a router is consulted:
//
//  1. Is this an address the migration table moved? An old address is not
//     mounted, so only a middleware ahead of the router can turn it into a
//     redirect instead of a 404.
//  2. Is the control plane served at this host at all? If not, the answer is
//     chi's own 404 — the same call with the same body and the same headers as
//     an address nobody mounted — because a control plane that answers 403, or
//     that answers 404 with a different shape, at its customer's host has
//     announced that it exists somewhere else.
//
// It runs inside a.headers, which is what makes the second answer the same
// answer: the verdict is this middleware's and the response is the host's.
func (a *API) address(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if to, ok := alias(r.URL.Path); ok {
			target := to
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			h := w.Header()
			h.Set("Cache-Control", noStore)
			h.Del("Vary")
			status := http.StatusFound
			if unsafeMethod(r.Method) {
				status = http.StatusTemporaryRedirect
			}
			h.Set("Location", target)
			w.WriteHeader(status)
			return
		}
		if SurfaceOf(r.Context()) == SurfaceOps && !a.servesOps(r.Host) {
			a.fail(w, r, http.StatusNotFound, "nothing is served at this address")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// servesOps is the host gate. An installation that names no host of its own
// serves no control plane anywhere: the surface mounts, every request is a 404,
// and boot said so once. Guessing "the operator's tenant, at whatever host it
// answers" would be the old design — a control plane reachable at every
// customer's address, apologising for it in a comment.
func (a *API) servesOps(host string) bool {
	return a.opsHost != "" && HostOnly(a.opts.Installation) == HostOnly(host)
}

// opsHost caches Options.Installation's normalised form, so the gate on the
// path of every request compares two already-normalised strings.
func (a *API) cacheOpsHost() {
	a.opsHost = HostOnly(a.opts.Installation)
}

// A public response is cacheable by default, which is the whole difference
// between a face and a workspace. One minute is long enough to absorb a person
// clicking a page twice and short enough that a correction reaches the next
// visitor almost immediately; it is a constant rather than a setting because a
// knob nobody has a reason to turn is a knob nobody tests.
const publicMaxAge = "public, max-age=60"

// The public write limit. Anonymous writes are the ones anybody can make, and
// the ones with no account to lock out, so they are counted by tenant and
// address. Sixty a minute is far past a person filling in a form and far below
// anything that is not a machine, and — like the limit's window — it is a
// constant: kit/limit's own budget bounds the cost of asking, and a deployment
// that wants a different number is a deployment that changes this line.
const (
	publicWriteLimit  = 60
	publicWriteWindow = time.Minute
	publicWriteKey    = "httpx/public-write"
)

// MountedRoute is one route as the composition describes it: what a projection,
// a boot log and a test read to know what is served where. It is a value rather
// than a query on the API because the answer is a list, and the order is
// composition order.
type MountedRoute struct {
	Method      string
	Path        string
	OperationID string
	Surface     Surface
	// Auth is the declaration's own rendering: "public", "signed_in",
	// "permission task:read", "operator_permission tenant:manage".
	Auth string
	// Module is the manifest name the route was mounted beneath.
	Module string
	// Page says the route answers a document rather than a value.
	Page bool
}

// Mounted is every route the composition mounted, in composition order, from
// the same recording Recorded reads. The surface is on it because every
// description of a route has to say which of the three doors it is behind —
// the document, the projection, and the boot log.
func (a *API) Mounted() []MountedRoute {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]MountedRoute, 0, len(a.mounts))
	for _, m := range a.mounts {
		out = append(out, m.route())
	}
	return out
}

// MountedOn reports whether the composition mounted anything at all on a
// surface. kit/app asks it once about the workspace, because a product with no
// workspace is not a product.
func (a *API) MountedOn(s Surface) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, m := range a.mounts {
		if m.surface == s {
			return true
		}
	}
	return false
}

// MountedBySurface counts the composition per surface, for the boot line that
// says what is served where.
func (a *API) MountedBySurface() map[Surface]int {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[Surface]int{}
	for _, m := range a.mounts {
		out[m.surface]++
	}
	return out
}

// A mount record is the surface, the module, the address and the declaration —
// the four things the compose gate needs to check against each other, kept
// beside the route rather than reconstructed from the OpenAPI document.
type mounted struct {
	surface Surface
	module  string
	method  string
	path    string
	auth    Auth
	page    bool
}

func (m mounted) route() MountedRoute {
	return MountedRoute{
		Method: m.method, Path: m.path, Surface: m.surface, Module: m.module,
		Auth: m.auth.String(), Page: m.page,
	}
}

// noteMount records a registration. Called from prepare, which every door
// runs, so a route cannot exist without being seen here.
func (a *API) noteMount(m mounted) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mounts = append(a.mounts, m)
}

// noteRefusal records a mount-time refusal to report with all the others. A
// panic inside the first Routes call would report one mistake and hide the
// rest, which is why the gate collects rather than aborts — the house rule of
// module.Validate, applied to routes.
func (a *API) noteRefusal(msg string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refusals = append(a.refusals, msg)
}

// claim answers whether this call took the surface's root. The first module in
// composition order gets it and everybody after is told false.
func (a *API) claim(s Surface, module string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.homes == nil {
		a.homes = map[Surface]string{}
	}
	if _, taken := a.homes[s]; taken {
		return false
	}
	a.homes[s] = module
	return true
}

// accepted is R1, R2 and R3 in one pure function: the decision table the mount
// gate runs and the only place the three surfaces' rules about the four
// declarations are written. It is a function rather than a spread of ifs in the
// gate so that the table can be tested case by case, and so the request chain
// and the gate cannot hold two opinions.
func accepted(s Surface, auth Auth) (ok bool, why string) {
	switch s {
	case SurfacePublic:
		switch auth.kind {
		case kindPublic:
			return true, ""
		case kindSignedIn:
			return false, "is mounted on the Public router and declares signed_in; the public surface resolves no session and sets no cookie, so nobody can satisfy it — mount it on the App router, or declare it Public()"
		case kindPermission:
			return false, fmt.Sprintf("is mounted on the Public router and declares permission %s; the public surface resolves no session and sets no cookie, so nobody can satisfy it — mount it on the App router, or declare it Public()", auth.permission)
		default:
			return false, fmt.Sprintf("is mounted on the Public router and declares operator_permission %s; the control plane is not a public page — mount it on the Ops router", auth.permission)
		}
	case SurfaceApp:
		// A public declaration on the workspace is legal and is the sign-in
		// door: a route cannot require the session it is the way to obtain. It
		// is not a hole that needs plugging with a second list, because the
		// tenant middleware admits an anonymous caller to exactly these routes
		// and to nothing else on this surface — and the declaration it reads is
		// the one written at the mount, which is what records the route here.
		// A list of paths that had to agree with those declarations would be a
		// second source of truth about the same doors, and it would drift the
		// way every such list drifts: silently, and towards being open.
		return true, ""
	case SurfaceOps:
		if auth.kind == kindPublic {
			return false, "is mounted on the Ops router and declares public; the control plane is served at the installation host only and admits no anonymous caller — mount it on the Public router"
		}
		if auth.kind != kindOperator {
			return false, fmt.Sprintf("is mounted on the Ops router and declares %s; the control plane admits only httpx.OperatorPermission — a route the installation owns declares whose it is, or it is not the installation's", auth.String())
		}
		return true, ""
	}
	return false, fmt.Sprintf("is mounted on a surface this package does not know (%q); one of Public, App or Ops names which door it is", string(s))
}

// validateSurfaces is the mount gate's half: every contradiction between a
// router and a declaration, every page on a JSON surface, every address two
// modules claimed, and the invariant that closes the design — the composed path
// of every recorded route classifies back to the surface it was mounted on, so
// the router and the chain cannot disagree.
//
// It reports everything it found, not the first thing, because whoever wrote
// the composition fixes it once.
func (a *API) validateSurfaces() []string {
	a.mu.Lock()
	mounts, refusals, homes := append([]mounted(nil), a.mounts...), append([]string(nil), a.refusals...), a.homes
	a.mu.Unlock()

	out := append([]string(nil), refusals...)
	seen := map[string]mounted{}
	for _, m := range mounts {
		where := m.method + " " + m.path
		if id := a.operationID(m); id != "" {
			where += " (" + id + ")"
		}
		if ok, why := accepted(m.surface, m.auth); !ok {
			out = append(out, "httpx: "+where+" "+why)
		}
		if m.page && m.surface == SurfaceOps {
			out = append(out, "httpx: "+where+" is a page on the Ops router; the control plane is a JSON surface — the operator's chrome is the workspace at the installation host, so mount it on the App router")
		}
		if got := classify(m.path); got != m.surface {
			out = append(out, fmt.Sprintf("httpx: %s takes %s from the %s router and classifies as %s; a route's address and its chain must agree — mount it on the %s router",
				where, m.path, m.surface, got, got))
		}
		// An alias is a redirect and never a second mount, which holds only if
		// nothing answers at the old address either. A route mounted there would
		// be spent ahead of itself — the surfaces middleware answers the redirect
		// first — so the module would ship a door nobody can open.
		if to, moved := alias(m.path); moved {
			out = append(out, "httpx: "+where+" is mounted where the migration table redirects to "+to+
				"; an alias is a redirect and not a second mount — mount the route at "+to+", or delete the row when the release that needs it is behind you")
		}
		if prior, dup := seen[m.method+" "+m.path]; dup {
			if m.path == surfaceRoot(m.surface, homes) {
				out = append(out, fmt.Sprintf("httpx: the %s root (%s) is claimed by module %q; module %q mounted %s there anyway — one claimant per composition, and Home tells you whether you have the claim",
					m.surface, m.path, prior.module, m.module, m.method))
			} else {
				out = append(out, fmt.Sprintf("httpx: %s is mounted by module %q and module %q on the %s surface", m.method+" "+m.path, prior.module, m.module, m.surface))
			}
			continue
		}
		seen[m.method+" "+m.path] = m
	}
	sort.Strings(out)
	return out
}

// operationID reads back the id huma settled on, so a refusal names the route
// the way the document names it. The recording holds the same pointers the
// request middleware reads, so this sees the finished operation.
func (a *API) operationID(m mounted) string {
	for _, op := range a.Recorded() {
		if op.Method == m.method && op.Path == m.path {
			return op.OperationID
		}
	}
	return ""
}

// AnonymousDoors lists the workspace addresses that answer a caller with no
// session: every App route that declares Public(), read back off the mounts the
// kernel recorded. Boot prints them, because "the workspace admits nobody by
// default" is only true once you can see who it admits.
func (a *API) AnonymousDoors() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, m := range a.mounts {
		if m.surface == SurfaceApp && m.auth.kind == kindPublic {
			out = append(out, m.method+" "+m.path)
		}
	}
	sort.Strings(out)
	return out
}

// MountedAt answers, for one exact address, which surface serves it and which
// module mounted it.
func (a *API) MountedAt(path string) (MountedRoute, bool) {
	for _, m := range a.Mounted() {
		if m.Path == path {
			return m, true
		}
	}
	return MountedRoute{}, false
}

// surfaceRoot is the address a surface's Home claim is about: the root a second
// module may not mount.
func surfaceRoot(s Surface, homes map[Surface]string) string {
	if _, taken := homes[s]; !taken {
		return ""
	}
	if s == SurfaceApp {
		return AppRoot
	}
	if s == SurfacePublic {
		return "/"
	}
	return ""
}
