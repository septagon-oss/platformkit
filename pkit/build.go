package pkit

// Build is the point where an app written in sentences becomes a running
// application, and the last place a composition can still be refused for free.
//
// The engine is kit/app: it checks a composition, migrates, opens the
// connection, builds the API and its gates, opens the transport, and serves or
// works. What this file adds is the step before it — the resolver's ordered
// modules, the app's own recorded choices and the ports the kernel cannot
// answer for itself, all answered before the first effect. Every method on App
// records; Build is what answers, with every problem at once (decision 0074
// rule 1), and "before any effect" is meant literally: no connection is opened,
// no migration runs, no row is written, no port is listened on.
//
// The one thing Build cannot move out of the engine is the registration that can
// only be made over an open connection: kit/app.buildAPI registers the modules'
// routes a third time, over the pool and the store it just opened, and refuses a
// callback that mounted differently there than it did on the dry pair. That
// refusal is still before the migration. The phases below say which claims are
// made before effects and which are the engine's.

import (
	"context"
	"errors"
	"fmt"
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

// Plan answers every problem with the composition, at once, and changes
// nothing. The phases run in this order, each before the first effect:
//
//	P1 settings   — an app with no name, a customisation set twice, an
//	               AskForAccess with one half missing, an environment nobody
//	               named, a Choose of a module Use never named
//	P2 resolve    — a._resolve(d): duplicates, missing or ambiguous providers,
//	               cycles, phase violations, FromDeployment inputs, a simulated
//	               implementation outside development
//	P3 dry build  — every module's build, its Puts counted, a contribution no
//	               composed module takes refused, and the kernel's own manifest
//	               gates (module.Expand, module.Validate) run over what the dry
//	               build produced, so the subscription gate answers here too
//	P4 ports      — the three required ports have exactly one provider each, and
//	               every recorded role grants a permission a composed module
//	               defines and none that reaches the control plane
//
// Nothing here opens a connection, migrates, writes, listens or dials a broker.
// The remaining checks — the pool, the transport choice, and the gate that
// every declared operation has an authorization — are kit/app's: New answers the
// first two before anything is opened and the third needs the open pool, so it
// stays in the effect phase (see Build's comment).
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
	dry.skin = Skin{Theme: a.theme, Home: a.frontDoor(), Copy: a.copy, mods: built}
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
	engine, _, err := a.newEngine(ctx, d, role)
	if err != nil {
		return err
	}
	return engine.Run(ctx)
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
// that door asks for both.
func (a *App) engine(ctx context.Context, d Deployment, role app.Role) (*Runtime, error) {
	engine, _, err := a.newEngine(ctx, d, role)
	if err != nil {
		return nil, err
	}
	rt, err := engine.Start(ctx)
	if err != nil {
		return nil, err
	}
	return &Runtime{rt: rt}, nil
}

// newEngine is Plan plus the engine's own checks: it is where a Deployment's
// configuration reaches kit/app, which answers about the pool, the transport the
// role would use and every manifest gate before anything is opened.
//
// Between the engine's constructor and the first effect sits one more answer:
// the route gates, over two dry registrations (app.Declarations). They are
// registration-time checks over httpx's recorder — an operation guarded by a
// permission no composed module defines, an operation that publishes an event no
// module promised, a workspace that mounts nothing, an address that names a
// prefix, and the two registrations that did not mount the same surface — and
// nothing in them reads the database, so decision 0074 rule 1 puts them before
// the migration rather than after it. Two registrations, not one, because the
// last of those answers needs both: one dry run cannot tell a callback that
// mounts the same routes every time from one behind a mount guard, and the
// comparison against the live registration happens after the engine opens the
// pool and the store, which is an effect. What stays on this side of the
// migration is only what genuinely needs the connection: the pool, the transport,
// and /ready's own probe of the schema it opened. The engine answers the same gates
// over that connection too, and migrates only after they answer, because a module's
// Routes callback runs twice on the dry side and once on the live one, and one that
// mounted differently between any two of those runs would otherwise serve a surface
// no gate ever read (app.registrationsAgree).
func (a *App) newEngine(ctx context.Context, d Deployment, role app.Role) (*app.App, *Planned, error) {
	p, err := a.Plan(d)
	if err != nil {
		return nil, nil, err
	}
	a.buildMu.Lock()
	defer a.buildMu.Unlock()
	if a.built {
		return nil, nil, fmt.Errorf("pkit: %s: Build: this app is already built; a new lifecycle needs a new App", a.name)
	}
	engine, err := a.startEngine(ctx, p, d, role)
	if err != nil {
		return nil, nil, fmt.Errorf("pkit: %s: Build: %w", a.name, err)
	}
	if err := engine.Declarations(); err != nil {
		// Refused before the first effect: nothing was migrated and nothing was
		// listened on, so the App is not built and the composition can be
		// corrected and built again.
		return nil, nil, fmt.Errorf("pkit: %s: Build: %w", a.name, err)
	}
	a.built = true
	return engine, p, nil
}

// startEngine is the engine's own constructor with the deployment's process
// fields filled in: the role, the two transport seams, the cache constructors and
// the installation's host. They come from the Deployment rather than the App
// because they are what a process knows and a composition does not.
func (a *App) startEngine(ctx context.Context, p *Planned, d Deployment, role app.Role) (*app.App, error) {
	opts := p.options
	opts.Role = role
	opts.Transports = d.Transports
	opts.Transport = d.Transport
	opts.Caches = d.Caches
	opts.Installation = app.Installation{Host: d.Config.Server.InstallationHost}
	return app.New(ctx, d.Config, p.built, opts)
}

// Runtime is a built application whose listener belongs to its caller. Build is
// its only constructor, and what it holds is kit/app's Runtime: the connection,
// the handler and, for a role that runs a worker half, the transport.
type Runtime struct{ rt *app.Runtime }

// Handler is the application's HTTP surface, role by role, exactly as kit/app
// answers it: tests drive this through httptest rather than a port of their own.
func (r *Runtime) Handler() http.Handler { return r.rt.Handler() }

// Work is the worker half. It refuses a role with no worker half, a Runtime
// Close has released and a second call, all of which are kit/app's refusals.
func (r *Runtime) Work(ctx context.Context) error { return r.rt.Work(ctx) }

// Close releases the transport and the connection and is safe to call twice.
func (r *Runtime) Close() error { return r.rt.Close() }

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
