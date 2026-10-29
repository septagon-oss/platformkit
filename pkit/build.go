package pkit

// Build is the point where an app written in sentences becomes a running
// application, and the last place a composition can still be refused for free.
//
// The engine is kit/app: it checks a composition, migrates, opens the connection,
// builds the API and its gates, opens the transport, and serves or works. This
// file is the step before it — the resolver's ordered modules, the app's recorded
// choices and the ports the kernel cannot answer for itself, all answered before
// the first effect, with every problem at once. "Free of both sides of the
// effect", what a refusal above the connection costs and what App.release tells
// apart, and why the route registrations run four times: pkit/README.md.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Authenticator answers who is calling. It is the engine's own shape
// (app.Options.Authenticate), spelled here so an app can name the port without
// importing the module that implements it: the module declares
// Provides[pkit.Authenticator]() and puts its own method, and the app never
// learns which module answered.
type Authenticator func(ctx context.Context, tx db.Tx[db.Tenant], r *http.Request) (tenancy.Principal, bool, error)

// The three ports every application must answer, as the kernel names them. Each
// is one contract type, so a module supplies one by declaring it and putting it
// — the same mechanism every other contract uses, and the reason no name of a
// module appears in this list.
//
// Entitle is the fourth and is not here: an application that sells nothing
// declares no feature and is never asked, so its absence is an answer rather
// than a hole. app.Options carries it, nil, and httpx is the one that refuses a
// composition that declared a feature and left it empty.
var requiredPorts = []struct {
	key      func() reflect.Type
	question string
	fix      string
}{
	{key: reflect.TypeFor[httpx.TenantLoader], question: "which host is which tenant",
		fix: "Put one in the module that knows which host is which tenant"},
	{key: reflect.TypeFor[httpx.Authorizer], question: "what they may do",
		fix: "Put one in the module that holds the grants"},
	{key: reflect.TypeFor[Authenticator], question: "who is calling",
		fix: "Put one in the module that opens sessions"},
}

// Planned is a composition that is fully resolved and answered and has done
// nothing: every module is built, every port has exactly one provider, every
// recorded choice has been read, and no connection has been opened.
//
// It exists because the reference application's own entry points need the
// modules and the services a composition resolved *before* they can be handed
// to anything — bootstrap creates the first tenant through the tenant service,
// migrate needs the migration sources, and a test needs a table of the routes —
// all of it without a database. Plan is Build's whole validate phase, exported;
// Build is Plan and then the engine's effects.
type Planned struct {
	app   *App
	plan  *plan
	built []module.Module
	// values and putBy are every contract a composed module put and who put it;
	// ports is the ones with exactly one answer, which is what Value reads.
	values  map[any][]any
	putBy   map[any][]*Module
	ports   map[any]any
	skin    Skin
	options app.Options
}

// Modules are the built manifests, in the order the composition is built in —
// which is the order the kernel mounts routes, generates screens and declares
// events.
func (p *Planned) Modules() []module.Module { return p.built }

// Options are the answers the kernel asks the application for, as
// kit/app.Options: the ports above, the failure page, the ask doors and the
// catalogues the app recorded. The four fields that belong to a process rather
// than to a composition — Role, Transports, Transport and Installation — are
// left for Server, which is where a process names itself.
func (p *Planned) Options() app.Options { return p.options }

// Skin is what the app recorded, plus the words the composed manifests chose
// for the permissions they name.
func (p *Planned) Skin() Skin { return p.skin }

// Value is the one value a composed module put for contract T, and whether
// exactly one did. It is how an application reads the services its own modules
// provided — the tenant service it bootstraps with, the mailer a test reads the
// sent mail out of — from the composition rather than from a second list.
func Value[T any](p *Planned) (T, bool) {
	v, ok := p.ports[reflect.TypeFor[T]()].(T)
	return v, ok
}

// answer is what a contract has: the values composed modules put for it and the
// modules that put them, in build order. One of each is the answer a port needs;
// none and many are the two refusals.
func (p *Planned) answer(key any) ([]any, []*Module) {
	return p.values[key], p.putBy[key]
}

// Plan answers every problem with the composition, at once, and changes nothing:
// the settings, the resolver, every module's dry build with the kernel's manifest
// gates over what it produced, and the required ports and recorded roles, in that
// order. pkit/README.md carries the table of what each phase answers.
//
// Nothing here opens a connection, migrates, writes, listens or dials a broker.
// The remaining checks — the pool, the transport choice, and the gate that every
// declared operation has an authorization — are kit/app's: New answers the first
// two before anything is opened and the third needs the open pool, so it stays in
// the effect phase (see Build).
func (a *App) Plan(d Deployment) (*Planned, error) {
	var errs []error
	if a.name == "" {
		errs = append(errs, errors.New("pkit: : NewApp: an app needs a name; every shared name carries it"))
	}
	errs = append(errs, a.settings()...)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	p, built, puts, err := a.compose(d)
	if err != nil {
		return nil, err
	}
	dry := &Planned{app: a, plan: p, built: built,
		values: map[any][]any{}, putBy: map[any][]*Module{}, ports: map[any]any{}}
	for _, m := range p.order {
		for key, vs := range puts[m] {
			dry.values[key] = append(dry.values[key], vs...)
			dry.putBy[key] = append(dry.putBy[key], m)
		}
	}
	for key, vs := range dry.values {
		if len(vs) == 1 && len(dry.putBy[key]) == 1 {
			dry.ports[key] = vs[0]
		}
	}
	for _, dl := range requiredPorts {
		key := dl.key()
		_, who := dry.answer(key)
		switch len(who) {
		case 1:
			// answered above: the value and the module that put it are in ports
		case 0:
			errs = append(errs, fmt.Errorf("pkit: %s: Build: no composed module provides %s, which is %s, so no request could be answered: %s",
				a.name, contract(key), dl.question, dl.fix))
		default:
			errs = append(errs, fmt.Errorf("pkit: %s: Build: %d composed modules provide %s — %s — which is %s: one module is the one that answers it, or Choose names which",
				a.name, len(who), contract(key), andList(moduleNames(who)), contract(key)+" "+dl.question))
		}
	}
	errs = append(errs, a.checkRoles(built)...)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	loader, _ := Value[httpx.TenantLoader](dry)
	authorizer, _ := Value[httpx.Authorizer](dry)
	entitler, _ := Value[httpx.Entitler](dry)
	authenticator, _ := Value[Authenticator](dry)
	dry.skin = a.recordedSkin()
	dry.skin.mods = built
	dry.options = app.Options{
		Tenants:   loader,
		Authorize: authorizer,
		Entitle:   entitler,
	}
	if fn := authenticator; fn != nil {
		dry.options.Authenticate = fn
	}
	if a.refusal != nil {
		dry.options.Fault = a.refusal(dry.skin)
	}
	if a.ask != nil {
		dry.options.Access = a.ask
		dry.options.AccessPage = a.askPage
	}
	dry.options.WorkspaceCatalog = a.catalog
	return dry, nil
}

// Build resolves the app for one deployment and, only once every problem is
// answered, runs the engine's effects in kit/app's own order: migrate, open the
// connection, build the API and its gates, open the transport. It is one Build
// per App, because the lifecycle kit/app documents is one per App: a second
// Start would migrate again over the same configuration and share a transport
// two Runtimes then both close.
func (a *App) Build(ctx context.Context, d Deployment) (*Runtime, error) {
	return a.engine(ctx, d, app.All)
}

// Run is Build and whichever half role names, until ctx is done. It is
// kit/app's Run: web serves, worker relays and consumes and ticks, all does both
// in one process and owns the listener.
func (a *App) Run(ctx context.Context, d Deployment, role app.Role) error {
	engine, _, claim, err := a.newEngine(ctx, d, role)
	if err != nil {
		return err
	}
	err = engine.Run(ctx)
	// The claim is released as the boot's last act rather than the first: until
	// Run returns this process still answers on this database as this application.
	claim.release()
	a.release(err)
	return err
}

// MustBuild is Build for tests and examples. It panics with Build's joined
// sentence and nothing else, which is the only reason it may exist: the caller
// has already decided that a composition that does not resolve is a defect in
// the test, and a panic is how a defect in a test announces itself.
func (a *App) MustBuild(ctx context.Context, d Deployment) *Runtime {
	rt, err := a.Build(ctx, d)
	if err != nil {
		panic(err.Error())
	}
	return rt
}

// engine is Build's two halves: newEngine, then kit/app's Start. It returns the
// started Runtime; Run keeps the engine and serves from it itself, which is why
// that door asks for both. The database claim newEngine took travels into the
// Runtime, whose Close is the release of both lifecycles, and is given back here
// when there is no Runtime to travel into.
func (a *App) engine(ctx context.Context, d Deployment, role app.Role) (*Runtime, error) {
	engine, _, claim, err := a.newEngine(ctx, d, role)
	if err != nil {
		return nil, err
	}
	rt, err := engine.Start(ctx)
	if err != nil {
		claim.release()
		a.release(err)
		return nil, err
	}
	return &Runtime{rt: rt, claim: claim}, nil
}

// release settles what a call to the engine's effects left this App. A boot
// kit/app refused above the connection — a route gate, the fourth registration,
// an operation gate, the cache segment the configuration names, the claim on an
// event name this process already answers under — was answered over an in-process
// store: nothing was dialled, migrated or listened on, so the
// App never had a lifecycle and the composition can be corrected and built again
// (README: a build refused before the first effect "changes nothing in the process
// it was asked in"). Any other refusal reached the pool, the store the deployment
// names, the migration or the transport, and this App keeps the one lifecycle it
// handed the engine whatever that boot gave back. The answer is app's, not a guess
// at which sentence came from where (see app.RefusedBeforeEffects).
func (a *App) release(err error) {
	if !app.RefusedBeforeEffects(err) {
		return
	}
	a.buildMu.Lock()
	defer a.buildMu.Unlock()
	a.built = false
}

// newEngine is Plan plus the engine's own checks: it is where a Deployment's
// configuration reaches kit/app, which answers about the pool, the transport
// the role would use and every manifest gate before anything is opened.
//
// It is also where this process records that this application, with this
// composition, is on this database: the claim is read after Plan, so a
// composition that does not resolve is refused by the sentences that caused it,
// and taken before startEngine, so a boot refused here leaves the database
// undialed. The held claim belongs to the lifecycle — a caller that starts
// nothing gives it back, and a Runtime carries it to Close (see claims.go).
//
// Between the engine's constructor and the first effect sit the route gates:
// three dry registrations and the API that would serve, none of which reads the
// database. pkit/README.md says why three and what each one refuses.
func (a *App) newEngine(ctx context.Context, d Deployment, role app.Role) (*app.App, *Planned, *held, error) {
	p, err := a.Plan(d)
	if err != nil {
		return nil, nil, nil, err
	}
	// What the cloud profile has to say about this deployment, said once, here, and
	// nowhere else: Plan and Explain are readings, and a reading that logged would
	// make the composition file a client of the log sink. Nothing has been opened at
	// this point, so a boot the engine goes on to refuse still heard its own
	// configuration described back to it.
	for _, w := range p.plan.cloud.warnings {
		slog.WarnContext(ctx, fmt.Sprintf("pkit: %s: %s", a.name, w))
	}
	a.buildMu.Lock()
	defer a.buildMu.Unlock()
	if a.built {
		return nil, nil, nil, fmt.Errorf("pkit: %s: Build: this app is already built; a new lifecycle needs a new App", a.name)
	}
	claim, err := claimDatabase(d, a.name, p.built)
	if err != nil {
		return nil, nil, nil, err
	}
	engine, err := a.startEngine(ctx, p, d, role)
	if err != nil {
		claim.release()
		return nil, nil, nil, fmt.Errorf("pkit: %s: Build: %w", a.name, err)
	}
	if err := engine.Declarations(); err != nil {
		// Refused before the first effect: nothing was migrated and nothing was
		// listened on, so the App is not built and the composition can be
		// corrected and built again. The fourth registration is answered inside
		// Start, still above the connection, and release gives this App back there
		// too — the flag is set now because a Start that reaches the pool spends
		// this App's lifecycle even when it comes back with no Runtime.
		claim.release()
		return nil, nil, nil, fmt.Errorf("pkit: %s: Build: %w", a.name, err)
	}
	a.built = true
	return engine, p, claim, nil
}

// startEngine is the engine's own constructor with the deployment's process
// fields filled in: the role, the two transport seams, the cache constructors and
// the installation's host. They come from the Deployment rather than the App
// because they are what a process knows and a composition does not. The
// configuration comes from the plan, because that is the one the cloud profile
// resolved and the modules read: an engine started on the settings as they arrived
// would run a different transport, cache and store from the ones Explain names.
func (a *App) startEngine(ctx context.Context, p *Planned, d Deployment, role app.Role) (*app.App, error) {
	opts := p.options
	opts.Role = role
	opts.Transports = d.Transports
	opts.Transport = d.Transport
	opts.Caches = d.Caches
	cfg := p.plan.cfg
	opts.Installation = app.Installation{Host: cfg.Server.InstallationHost}
	return app.New(ctx, cfg, p.built, opts)
}

// Runtime is a built application whose listener belongs to its caller. Build is
// its only constructor, and what it holds is kit/app's Runtime: the connection,
// the handler and, for a role that runs a worker half, the transport — beside the
// process's claim that this application is the one on this database.
type Runtime struct {
	rt    *app.Runtime
	claim *held
}

// Handler is the application's HTTP surface, role by role, exactly as kit/app
// answers it: tests drive this through httptest rather than a port of their own.
func (r *Runtime) Handler() http.Handler { return r.rt.Handler() }

// Work is the worker half. It refuses a role with no worker half, a Runtime
// Close has released and a second call, all of which are kit/app's refusals.
func (r *Runtime) Work(ctx context.Context) error { return r.rt.Work(ctx) }

// Close releases the transport and the connection and is safe to call twice.
// The database claim goes with them, and after them: the application this process
// was on that database stops being this process's the moment the connection that
// made it one is released.
func (r *Runtime) Close() error {
	err := r.rt.Close()
	r.claim.release()
	return err
}

// settings is P1: the recorded choices, read for contradiction. No method on App
// answers anything while it is being called (0074 rule 2), so this is the first
// moment any of it is observable — and it is the reason a chain that names Theme
// twice says so instead of keeping the last one.
func (a *App) settings() []error {
	var errs []error
	for _, named := range []struct {
		what  string
		times int
	}{
		{"Theme", a.themes},
		{"Languages", a.copies},
		{"ErrorPage", a.refusals},
	} {
		if named.times > 1 {
			errs = append(errs, fmt.Errorf("pkit: %s: %s is set twice: the last one is what the app would do, and that is not what you said",
				a.name, named.what))
		}
	}
	if len(a.homes) > 1 {
		errs = append(errs, fmt.Errorf("pkit: %s: Home is set twice, %s then %s: one app has one front door",
			a.name, a.homes[0], a.homes[1]))
	}
	if home := a.frontDoor(); home != "" && !strings.HasPrefix(home, "/") {
		errs = append(errs, fmt.Errorf("pkit: %s: Home(%q) is not a path beginning with /", a.name, home))
	}
	if (a.ask == nil) != (a.askPage == nil) {
		which := "form"
		if a.askPage == nil {
			which = "reach"
		}
		errs = append(errs, fmt.Errorf("pkit: %s: AskForAccess has no %s: a reach with no page is a refusal that offers nothing, and a page with no reach is a button that lies", a.name, which))
	}
	return errs
}

// frontDoor is the one Home this app named, and "" when it named none: a page
// with no way on is a page that offers nothing, which the refusal page already
// answers by drawing no link rather than by refusing the boot.
func (a *App) frontDoor() string {
	if len(a.homes) == 0 {
		return ""
	}
	return a.homes[len(a.homes)-1]
}

// checkRoles is P4's second half: a role every tenant starts with may grant only
// what a composed module defines, and never what reaches the control plane. This
// is the reference application's checkPersonas, which panics; here it is a
// refusal with the same two answers, read off the same manifests, because a
// composition that grants a permission nobody defines is a role that lies and a
// lie is a sentence, not a crash.
func (a *App) checkRoles(built []module.Module) []error {
	if len(a.roles) == 0 {
		return nil
	}
	defined := map[string]tenancy.Grant{}
	for _, m := range built {
		for _, p := range m.Permissions {
			defined[p.Key] = tenancy.Grant{Permission: p.Key, Operator: p.Operator}
		}
	}
	var errs []error
	for _, r := range a.roles {
		for _, key := range r.Grants {
			g, ok := defined[key]
			switch {
			case !ok:
				errs = append(errs, fmt.Errorf("pkit: %s: Roles: %s grants %s, which no composed module defines: compose the module that defines it or do not grant it",
					a.name, r.Name, key))
			case g.Operator:
				errs = append(errs, fmt.Errorf("pkit: %s: Roles: %s grants %s, which reaches the control plane: a tenant's role may not",
					a.name, r.Name, key))
			}
		}
	}
	return errs
}
