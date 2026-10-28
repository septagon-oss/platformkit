package httpx

// Surfaces compose module-relative JSON and document addresses on one router.
// Classification drives both mount validation and request guards so a declared
// surface and its served address agree. See README.md for prefixes and homes.

import (
	"context"
	"net/http"
	"net/url"
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
	// opsJSONSegment is the control plane's segment of the JSON address. It is a
	// name rather than a literal because the surface question is asked in two places
	// — here, where the address is composed, and kit/httpx/idempotency.go, which
	// decides what the document declares and can only read the address.
	opsJSONSegment = "ops"
)

// isOpsPath identifies control-plane addresses for classification and admission.
// Unclassified API paths use workspace guards; other paths default to Public,
// which admits no principal-dependent route. The mount gate and request chain
// use this same path-only classification.
func isOpsPath(path string) bool {
	return path == OpsRoot || strings.HasPrefix(path, OpsRoot+"/") ||
		strings.HasPrefix(path, apiRoot+"/ops/")
}

func classify(path string) Surface {
	switch {
	case isOpsPath(path):
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

// UnderAPI reports whether a link to the path p opens the JSON API's root or an
// address beneath it. Every module route and the public JSON answer there and no
// page does, so a composition that names a page of its own is refused one this
// answers true for.
//
// p is read as a browser reads a link: its dot segments are resolved before the
// request is sent, percent-encoded ones too ("%2e%2e"), and ".." takes back an
// empty segment rather than collapsing it as path.Clean would, so
// "/api/v1//../auth" opens "/api/v1/auth". What is left is percent-decoded the
// way a router reading the unescaped path sees it.
func UnderAPI(p string) bool {
	var opened []string
	for _, segment := range strings.Split(p, "/")[1:] {
		if decoded, err := url.PathUnescape(segment); err == nil {
			segment = decoded
		}
		switch segment {
		case ".":
		case "..":
			opened = opened[:max(len(opened)-1, 0)]
		default:
			opened = append(opened, segment)
		}
	}
	p = "/" + strings.Join(opened, "/")
	return p == apiRoot || strings.HasPrefix(p, apiRoot+"/")
}

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
	s.Public = &Router{surface: SurfacePublic, module: module, screenOwner: module, api: a}
	s.App = &Router{surface: SurfaceApp, module: module, screenOwner: module, api: a}
	s.Ops = &Router{surface: SurfaceOps, module: module, screenOwner: module, api: a}
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

// SystemToken is the credential an in-process caller uses to reach a route that
// needs a principal where no HTTP request carries one. It belongs to the
// composition, not to a surface: which router it is presented to is the caller's
// business. See the API's field of the same name.
func (s Surfaces) SystemToken() tenancy.SystemToken { return s.App.SystemToken() }

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
		s := a.surfaceOf(r.URL.Path)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), surfaceKey{}, s)))
	})
}

// address handles migrated addresses before routing, then conceals Ops on hosts
// that do not serve it. It runs inside headers so its 404 matches an unmounted
// address in both response shape and security headers.
func (a *API) address(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if to, ok := a.alias(r.URL.Path); ok {
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

	// publicWritePolicy is the same limit under the name the wire gives it. A policy
	// name is part of what `RateLimit-Policy` promises to keep consistent across a
	// sequence of responses, so it lives beside the numbers it is the quota of: a
	// client that sees "public-writes" in one answer and "writes" in the next has been
	// given two limits rather than one stated twice.
	publicWritePolicyName = "public-writes"
)

// publicWritePolicy is the limit above, as a policy the response can name.
var publicWritePolicy = limitPolicy{publicWritePolicyName, publicWriteLimit, publicWriteWindow}
