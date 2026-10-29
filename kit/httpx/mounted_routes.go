package httpx

import (
	"context"
	"fmt"
	"sort"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/screen"
)

// Register mounts an operation on this router, together with the authorization
// it declares, at the address the table gives it. It is the only way a module
// registers a handler.
//
// The handler is wrapped to record the refusal it answered with, because the answer this
// package writes for a caller that came to look at a page is decided from the verdict, and
// two facts about a verdict do not reach the JSON body a page would otherwise be read off.
// See buffer.carried and noteCarried.
func Register[I, O any](r *Router, op huma.Operation, auth Auth, handler func(context.Context, *I) (*O, error)) {
	op.Path = r.compose(op.Path, false)
	prepare(r, &op, auth, false)
	api := newIntegerAPI[I, O](r.api)
	huma.Register(api, op, func(ctx context.Context, in *I) (*O, error) {
		out, err := handler(ctx, in)
		noteCarried(ctx, err)
		return out, err
	})
	api.documentIntegers()
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
	contract, _ := op.Extensions[screenExtension].(screen.ResponseSet)
	r.api.noteMount(mounted{operationID: op.OperationID, screenOwner: r.screenOwner, screens: contract, surface: r.surface, module: r.module, method: op.Method, path: op.Path, auth: auth, page: page})
}

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
	// "any_credential", "permission task:read", "operator_permission
	// tenant:manage".
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
	operationID, screenOwner string
	screens                  screen.ResponseSet
	surface                  Surface
	module                   string
	method                   string
	path                     string
	auth                     Auth
	page                     bool
	static                   bool
}

func (m mounted) route() MountedRoute {
	return MountedRoute{
		OperationID: m.operationID, Method: m.method, Path: m.path, Surface: m.surface, Module: m.module,
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
		case kindAnyCredential, kindBearerKey:
			return false, "is mounted on the Public router and declares " + auth.String() + "; the public surface resolves no caller at all, so this door has nobody to admit — mount it on the App router, or declare it Public()"
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
	a.frozen = true
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
		if got := a.surfaceOf(m.path); got != m.surface {
			out = append(out, fmt.Sprintf("httpx: %s takes %s from the %s router and classifies as %s; a route's address and its chain must agree — mount it on the %s router",
				where, m.path, m.surface, got, got))
		}
		// An alias is a redirect and never a second mount, which holds only if
		// nothing answers at the old address either. A route mounted there would
		// be spent ahead of itself — the surfaces middleware answers the redirect
		// first — so the module would ship a door nobody can open.
		if to, moved := a.alias(m.path); moved {
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
