package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/stream"
)

// composeRoutes registers and validates one API without opening deployment
// resources. Routes receive the same disconnected surfaces on every pass;
// registrationsAgree compares module routes before kernel routes are mounted.
func (a *App) composeRoutes(store cache.Cache) (*httpx.API, *chi.Mux, error) {
	api, router := httpx.New(a.routeOptions(store))

	// The catalogue before the routes: a module that validates a permission
	// somebody typed asks the kernel for the list, and Routes is the one moment
	// it is being wired and the whole composition is known. It is
	// module.Grants and not a loop written here, because a composition seeding
	// a tenant's roles needs the same list before it opens a database, and two
	// copies of it are two answers to one question.
	api.Declare(module.Grants(a.mods))

	for _, m := range a.mods {
		// A moved address is registered before any route, so the surface gate below
		// can refuse a route mounted where a row redirects.
		for _, mv := range m.Moved {
			api.Alias(mv.From, mv.To)
		}
	}
	for _, m := range a.mods {
		if m.Routes != nil {
			m.Routes(api.Surfaces(m.Name))
		}
	}
	// The same line of the same sequence on both passes: every module's Routes has
	// run, and composeGates has not yet mounted the routes the kernel owns, nor
	// ValidateDeclarations stamped a surface onto huma's own schema route — what the
	// kernel mounts it mounts alike on both runs, and a module's callback is the one
	// thing they can disagree about. Before every gate below, each of which reads the
	// routes as they are and would answer a disagreement as some other composition's
	// problem: "this composition mounts nothing".
	if err := a.registrationsAgree(api); err != nil {
		return nil, nil, err
	}
	if err := a.composeGates(api); err != nil {
		return nil, nil, err
	}
	// The two delivery verbs the installation owns, and only for a composition that
	// names itself: an app-less deployment has no scoped durable to move onto and no
	// app to name in the record either verb writes, so both doors would open onto a
	// zero report. mountLedgerOps and kernelModule carry the same condition.
	if a.opts.App.Named() {
		mountLedgerOps(api, a.opts.App)
	}
	// One check, and the reason there is one is that /ready answers a question a
	// probe can act on: is this instance's database reachable. Modules used to
	// be able to contribute their own and none ever did in three repositories,
	// so the list was this line and a loop over nothing. The probe asks the held
	// connection rather than one captured here, because this line runs in a boot
	// that has not opened its pool yet and /ready is only ever asked of a boot
	// that has.
	mountOperations(api, a.opts.Clock)
	health.Register(api, []health.Check{health.DatabaseCheckThrough(a.held.read)}, a.reports...)

	// The gates. An operation that declares no authorization, one guarded by a
	// permission no module defines, or one that would publish an event no
	// module promised, fails startup — with no warn mode: this repository has
	// no deployment to migrate and no reason to run a build it knows is
	// unprotected, unreachable or unannounced.
	if err := a.validateRoutes(api); err != nil {
		return nil, nil, err
	}
	return api, router, nil
}

// registeredRoutes is what one registration of this composition mounted, as one
// sorted set of lines rather than a call order. The composition writes every
// route into two readings of itself and the set carries both, because neither
// alone names what a module's Routes callback decided. The mount table is every
// address a router mounted — routes and file trees alike — with its surface, its
// module and the authorization guarding it; a tree mounted with Surfaces.Static
// is recorded there and nowhere else, because it is no operation, so the recorder
// never saw it and no operation gate ever will. The operation recorder adds what
// only an operation carries — the id the OpenAPI document names it by, which is
// what validateEvents and validatePermissions read — and names the kernel's own
// routes, which no module mounted and which the kernel mounts alike on both
// sides. A module's Routes callback writes both readings, and the kernel and the
// manifests write them the same way in every registration, which makes them the
// whole of what any two registrations can disagree about.
func registeredRoutes(api *httpx.API) []string {
	recorded, mounted := api.Recorded(), api.Mounted()
	lines := make([]string, 0, len(recorded)+len(mounted))
	for _, op := range recorded {
		lines = append(lines, fmt.Sprintf("operation %s %s (%s) guarded by %v",
			op.Method, op.Path, op.OperationID, op.Extensions[httpx.AuthExtension]))
	}
	for _, m := range mounted {
		kind := "value"
		if m.Page {
			kind = "document"
		}
		lines = append(lines, fmt.Sprintf("mount %s %s on %s by %s, a %s, guarded by %s",
			m.Method, m.Path, m.Surface, m.Module, kind, m.Auth))
	}
	sort.Strings(lines)
	return lines
}

// registrationsAgree makes every registration of one composition's routes one
// claim rather than a hope. They are registered four times: three times on a
// recorder, where Declarations answers the gates before any effect, and once more
// for the API that will serve, which is built the same way, over an in-process
// store, and is handed the deployment's connection and store only once it has
// answered. kit/module.Module.Routes never asked a callback to answer the same way
// twice — kit/app called each one exactly once before the recorder existed — so a
// callback behind an idempotent guard answers differently and one pass cannot see
// it. The first registration therefore sets the standard and every later one is
// judged against it, naming the side that differs. All four answers land before the
// pool, the shared store and the migration are touched, which is what makes the
// fourth one worth having: it is the registration whose surface would serve, and a
// boot it refuses has spent nothing to find out.
func (a *App) registrationsAgree(api *httpx.API) error {
	if a.declaredRoutes == nil {
		a.declaredRoutes = registeredRoutes(api)
		return nil
	}
	first := a.declaredRoutes
	live := registeredRoutes(api)
	var bad []string
	for _, gone := range onlyIn(first, live) {
		bad = append(bad, "mounted by the first registration and not by this one: "+gone)
	}
	for _, added := range onlyIn(live, first) {
		bad = append(bad, "mounted by this registration and not by the first: "+added)
	}
	if len(bad) == 0 {
		return nil
	}
	return errors.New("app: invalid composition:\n  this composition registered its routes more than once and answered more than once:\n    " +
		strings.Join(bad, "\n    ") +
		"\n  a module's Routes callback runs four times in one boot — three times while the gates are answered, once more for the API that is built to serve; a callback that mounts differently on any two of those runs is a composition that means two different things — mount the same routes every time Routes is called")
}

// onlyIn is every line the first registration carries that the second does not,
// repeats included: the same route mounted twice is not the same composition as
// that route mounted once.
func onlyIn(first, second []string) []string {
	count := make(map[string]int, len(second))
	for _, line := range second {
		count[line]++
	}
	var out []string
	for _, line := range first {
		if count[line] == 0 {
			out = append(out, line)
			continue
		}
		count[line]--
	}
	return out
}

// buildAPI is composeRoutes for the one API this process will serve, plus what
// boot prints once the composition is known to be good. It is the fourth
// registration and the last: it is built over an in-process store, with no
// connection, exactly as the three gate passes were, so a composition that fails a
// gate here — or whose modules mounted differently on this pass — is refused before
// the pool, the shared store and the migration, and never takes the port. Start
// hands what it returns to httpx.API.Connect once it has opened both.
func (a *App) buildAPI(ctx context.Context, store cache.Cache) (*httpx.API, http.Handler, error) {
	api, router, err := a.composeRoutes(store)
	if err != nil {
		return nil, nil, err
	}
	counts := api.MountedBySurface()
	// event_schema_coverage is the register's number for this pillar: how many
	// of the events this composition emits carry a payload schema its module
	// declared. It is printed at boot because a number nobody can read is a
	// number that drifts back down.
	covered, declared := CoveredEvents(a.mods)
	a.log.InfoContext(ctx, "app: operations declared",
		"count", len(api.Recorded()), "events", len(api.Events()),
		"public", counts[httpx.SurfacePublic], "app", counts[httpx.SurfaceApp], "ops", counts[httpx.SurfaceOps],
		"event_schema_coverage", fmt.Sprintf("%d/%d", covered, declared))
	if a.opts.Installation.Host == "" {
		a.log.WarnContext(ctx, "app: no installation host; the control plane (/ops) is mounted and answers nothing",
			"fix", "server.installation_host names the host the installation itself is reached at")
	}
	// The workspace admits nobody by default, which is only a true sentence
	// about a composition once you can see the doors that admit everybody.
	for _, door := range api.AnonymousDoors() {
		a.log.InfoContext(ctx, "app: a workspace route answers an anonymous caller", "route", door)
	}
	return api, router, nil
}

// composeGates refuses the compositions that cannot be products, and mounts the
// one route the composition itself owns. Every message here is a wiring mistake
// somebody can fix in the file that wrote it, and every one is collected rather
// than returned first — the house rule of module.Validate, applied to the
// composition's own rules.
func (a *App) composeGates(api *httpx.API) error {
	var bad []string
	add := func(format string, args ...any) { bad = append(bad, fmt.Sprintf(format, args...)) }

	// A2. A product without a workspace is not a product: the surface is where
	// a person stands, and a composition that mounts nothing there is an API
	// with a front door painted on it.
	//
	// The worker role is exempt, and is not an exception written to make a test
	// pass: it composes no HTTP surface at all — it serves two probes on a
	// handler nobody routes — so "your product has no workspace" is not a fact
	// about it. The role that listens, including All, is refused.
	if a.opts.Role != Worker && !api.MountedOn(httpx.SurfaceApp) {
		add("app: this composition mounts nothing on the workspace surface (%s); a product without a workspace is not a product — compose a module that registers routes on r.App (ui/screens.Mount for generated screens, modules/admin for the shell)", httpx.AppRoot)
	}

	// A5, and the route itself. The address is the kernel's; the body is the
	// composition's, because kit may not import ui.
	resources := api.Resources()
	if a.opts.Access != nil {
		mountAccessRequest(api)
		if a.opts.AccessPage != nil {
			a.opts.AccessPage(api.Surfaces("").App)
		}
	}
	// The live door, on the same condition the catalog is mounted on: a worker
	// process serves no HTTP surface at all, so it has no connection to hold open.
	if a.opts.Role != Worker {
		mountStream(api, a.streamHub(stream.NewRegistry(a.mods)), a.log)
	}
	if a.opts.WorkspaceCatalog != nil {
		a.opts.WorkspaceCatalog(api)
	} else if len(resources) > 0 && a.opts.Role != Worker {
		// Same exemption: the document is read by a shell, and a worker process
		// runs none. A web role that registered resources and wired no renderer
		// is the wiring mistake this refuses.
		add("app: %d resources are registered and the composition supplies no WorkspaceCatalog; a native shell has nothing to read — wire app.Options.WorkspaceCatalog (ui/screens.Describe renders the document)", len(resources))
	}

	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return errors.New("app: invalid composition:\n  " + strings.Join(bad, "\n  "))
}

// WorkspaceCatalogRoute mounts the composition's catalog at the kernel-owned
// address. describe supplies the document; T preserves its concrete response
// schema for OpenAPI without importing the presentation layer.
func WorkspaceCatalogRoute[T any](describe func(ctx context.Context, resources []httpx.Resource) (*T, error)) func(api *httpx.API) {
	return func(api *httpx.API) { mountWorkspaceCatalog(api, describe) }
}

// mountWorkspaceCatalog mounts the catalog route. It is the composition's own
// route: the address belongs to the kernel, which is why the mount is here, and
// the document belongs to ui, which is why the body is a function — and why that
// function's result type is carried through to huma's schema instead of being
// handed over as `any`.
// workspaceDocument is the catalog route's envelope: whatever the composition's
// function returned, as the body. It is generic for the same reason the mount is:
// the type has to survive to huma's schema. It is not simply dropped, because a
// response struct whose fields are not inside a Body is read by huma as a set of
// headers — the document would have left the OpenAPI description and then the wire.
type workspaceDocument[T any] struct {
	// Vary is the header the body was negotiated with: two callers of one address
	// holding two `Accept-Language` lines get two bodies, so a cache that stores one
	// must key it by the other. Nothing in this build caches the response —
	// kit/httpx answers `no-store` on every non-public surface — which is why the
	// field is written for the next consumer rather than the last, and why it names
	// only the header that actually changes the bytes.
	Vary string `header:"Vary"`
	Body *T
}

func mountWorkspaceCatalog[T any](api *httpx.API, describe func(ctx context.Context, resources []httpx.Resource) (*T, error)) {
	kernel := api.Surfaces("")
	httpx.Register(kernel.App, huma.Operation{
		OperationID: "app-resources",
		Method:      http.MethodGet,
		Path:        "/resources",
		Summary:     "The resources this caller may reach, with their schemas",
		Description: "The same document the generated screens are built from, for a shell that is not a browser.",
		Tags:        []string{"kernel"},
		// AnyCredential, not SignedIn, and the difference is the whole point of
		// the declaration: this route spends no authority on the caller's own
		// credentials — it describes what the caller may already reach, and the
		// document is filtered by the caller's grants as it is built — so the
		// caller may arrive with whichever credential they hold. The credential a
		// client that is not a browser holds is a bearer key, and this is that
		// client's first request: a key refused here could call every route its
		// scopes name and no route that says what those are.
	}, httpx.AnyCredential(), func(ctx context.Context, _ *struct{}) (*workspaceDocument[T], error) {
		body, err := describe(ctx, api.Resources())
		if err != nil {
			return nil, err
		}
		return &workspaceDocument[T]{Vary: "Accept-Language", Body: body}, nil
	})
}

func (a *App) routeOptions(store cache.Cache) httpx.Options {
	return httpx.Options{
		Unwired:        true,
		Cache:          store,
		PublicHost:     a.cfg.Server.PublicHost,
		Docs:           a.cfg.Server.Docs,
		Tenants:        a.opts.Tenants,
		Authorize:      a.opts.Authorize,
		Entitle:        a.opts.Entitle,
		Authenticate:   a.opts.Authenticate,
		JSONMediaTypes: a.opts.JSONMediaTypes,
		Fault:          a.opts.Fault,
		Log:            a.log,
		// The ceiling on the one route that reads its own request. Every other
		// body is bounded at httpx.MaxBodyBytes, and a schema'd one is bounded
		// by huma as well.
		MaxUpload: a.cfg.Files.MaxBytes,
		// The two facts of the surfaces the kernel cannot see for itself: which
		// host the installation owns, and who counts what an anonymous visitor
		// may write. Which workspace doors answer an anonymous caller is not
		// here, because the routes say it and the kernel records it; boot prints
		// the list below.
		Installation: a.opts.Installation.Host,
		WriteLimiter: limit.PostgresFor("", a.held.read),
		// The memory beside the counter: the same connection the limiter is counted
		// on, the same detached write, the same reason — a retried write arrives at
		// whichever replica the load balancer chose, so the record of the first one
		// has to live somewhere both of them can read.
		Idempotency: idempotency.Postgres(a.held.read),
		Operations:  idempotency.PostgresOperations(),
		Clock:       a.opts.Clock,
		// Every attributable refusal is an event and, where modules/audit is composed, an
		// audit row: see recordDenial.
		Denied: recordDenial(a.held, a.log),
		// The ask for access: the reach is the composition's, the record is the
		// kernel's event, and the one limiter the composition already builds.
		Access: a.opts.Access, Accessed: recordAccessRequest,
	}
}

func (a *App) validateRoutes(api *httpx.API) error {
	if err := api.ValidateDeclarations(); err != nil {
		return err
	}
	if err := validatePermissions(api, a.mods); err != nil {
		return err
	}
	if err := validateEvents(api, a.mods); err != nil {
		return err
	}
	return a.validateScreens(api)
}
