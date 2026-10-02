package pkit

// NewServer is where a process names itself. An app is a name and the modules it
// uses; a server is that app, the configuration it runs under, which half of the
// work this process does, which brokers it can reach, and the tenants it is
// reached for (decision 0074 rule 6).
//
// The division is deliberate: everything about a composition is app-level and
// resolved once, at boot; everything about a process is recorded here and read
// once, when Build runs. Neither half changes while requests are being served.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// TenantValue is one tenant's claim about where an application is reached: its
// name and one host. Tenant builds it; nothing else does.
type TenantValue struct {
	Name string
	Host string
}

// Tenant claims that this application is reached for this tenant at this host.
//
// It is a claim and not a mapping, a route or a row. The answer to "which tenant
// is this host?" stays the tenant module's rows — tenant_hosts, read by
// httpx.TenantLoader per request, which is the tenancy boundary the database
// enforces (decision 0052 §4). A second answer written here would be a second
// question the same request has to reconcile, and the two would drift the first
// time an operator adds a host. What a claim is for is three things: refusing one
// host claimed by two tenants, refusing two applications on one database, and
// writing the tenant-hosts section of the composition file, which nothing else
// can say because nothing else knows what a deployment intends.
func Tenant(name, host string) TenantValue { return TenantValue{Name: name, Host: host} }

// Server is a process: the applications it hosts and the configuration, role and
// brokers it runs them under. Every method records; Build answers, and Run is
// Build and whichever half role names.
type Server struct {
	deploy Deployment
	set    bool
	role   app.Role
	hosts  []hosted
}

type hosted struct {
	app     *App
	tenants []TenantValue
}

// NewServer starts a process description.
func NewServer() *Server { return &Server{} }

// Config is the process's configuration — kit/config's own value, read by the
// engine for the pool, the transport's broker settings, the log level and the
// installation's own host. It is the process's, not the composition's: the same
// app description answers under two configurations, which is what rule 4 is for.
func (s *Server) Config(cfg config.Config) *Server {
	s.deploy.Config = cfg
	if cfg.Server.Addr != "" || cfg.Database.URL != "" {
		s.set = true
	}
	return s
}

// Deploy names the environment this process is and the inputs a module picked by
// FromDeployment reads. It is where a process says which environment it is in;
// the composition never learns it from a build tag.
func (s *Server) Deploy(d Deployment) *Server {
	if d.Config.Database.URL == "" {
		d.Config = s.deploy.Config
	}
	if d.Transports.Memory == nil && d.Transports.JetStream == nil {
		d.Transports = s.deploy.Transports
	}
	s.deploy, s.set = d, true
	return s
}

// Role is which half of the application this process runs: web, worker or all.
// One image; the role is a flag (docs/adr/0005). Left unset it is all.
func (s *Server) Role(role app.Role) *Server { s.role = role; return s }

// Transports are the two event-transport constructors the configuration can
// name. kit/app knows the names and the rule between them and builds neither,
// so the process that links the provider packages supplies them here and nowhere
// else.
func (s *Server) Transports(t app.Transports) *Server {
	s.deploy.Transports = t
	s.set = true
	return s
}

// Transport is the explicit override kit/app.Options carries: the process that
// already holds a transport hands it over instead of naming a constructor.
func (s *Server) Transport(t events.Transport) *Server {
	s.deploy.Transport = t
	s.set = true
	return s
}

// Host records that this process serves this application, and the tenants it is
// reached for. It validates nothing (0074 rule 2), provisions nothing and routes
// nothing: which host is which tenant is answered per request by the tenant
// module's rows, through the loader the composition provided.
func (s *Server) Host(a *App, tenants ...TenantValue) *Server {
	s.hosts = append(s.hosts, hosted{app: a, tenants: tenants})
	return s
}

// Served is a built process: the applications it hosts, with their lifecycles
// started and their claims recorded.
type Served struct {
	runtimes []*Runtime
	hosts    []hosted
	planned  []*Planned
}

// Handler is the served process's HTTP surface. With one application hosted it is
// that application's handler; choosing between two over one listener is what
// T-0231 buys, and until then there is nothing to choose between.
func (s *Served) Handler() http.Handler {
	if len(s.runtimes) == 0 {
		return http.NotFoundHandler()
	}
	return s.runtimes[0].Handler()
}

// Close releases every hosted application.
func (s *Served) Close() error {
	var errs []error
	for _, rt := range s.runtimes {
		if err := rt.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Work runs the worker half of every hosted application. A server that hosts one
// app works one database, which is the arrangement kit/app supports.
func (s *Served) Work(ctx context.Context) error {
	if len(s.runtimes) == 0 {
		return errors.New("pkit: server: Work with nothing hosted")
	}
	return s.runtimes[0].Work(ctx)
}

// roleOrDefault is which half this process runs, with the default named: a
// process that never said runs the whole application. Build, Run and Explain each
// read it, because all three say what the process does.
func (s *Server) roleOrDefault() app.Role {
	if s.role == "" {
		return app.All
	}
	return s.role
}

// hostedNames is every application this process claims to be, in the order they
// were hosted. Each place that refuses a process for what it hosts has to name
// them, and an error that names the wrong application is worse than one that
// names none.
func (s *Server) hostedNames() []string {
	var names []string
	for _, h := range s.hosts {
		names = append(names, h.app.name)
	}
	return names
}

// claim is one tenant's host as the collision check holds it: which tenant claims
// it, spelled as the Host call spelled it. The map holding claims is keyed by the
// canonical host the request path sees; the spelling is kept because a refusal that
// quotes the two claims that collided is the answer an operator can act on.
type claim struct{ tenant, spelled string }

// refusals is the process's own half of Build: every problem with what it hosts,
// what it was given, and what its tenants claim, answered before anything is
// opened. It is separate from the effects because Run needs exactly this answer
// without starting an engine it is about to run itself.
func (s *Server) refusals() error {
	var errs []error
	if !s.set {
		errs = append(errs, errors.New("pkit: server: Build needs a Deployment — say which environment this process is"))
	}
	if s.deploy.Config.Database.URL == "" {
		errs = append(errs, errors.New("pkit: server: Build needs a Config — the Deployment names the environment, the configuration names the database"))
	}
	if len(s.hosts) == 0 {
		errs = append(errs, errors.New("pkit: server: Build hosts nothing: Host(app) first"))
	}
	seen := map[*App]string{}
	claims := map[string]claim{}
	for _, h := range s.hosts {
		if prior, twice := seen[h.app]; twice && prior == h.app.name {
			errs = append(errs, fmt.Errorf("pkit: %s: Host: %s is hosted twice; one call names its tenants", h.app.name, h.app.name))
		}
		seen[h.app] = h.app.name
		for _, t := range h.tenants {
			switch {
			case t.Name == "":
				errs = append(errs, fmt.Errorf("pkit: %s: Tenant(%q, %q): a tenant with no name cannot be the tenant a host belongs to", h.app.name, t.Name, t.Host))
			case t.Host == "":
				errs = append(errs, fmt.Errorf("pkit: %s: Tenant(%q, %q): a host with no name cannot belong to anybody", h.app.name, t.Name, t.Host))
			}
			// The key is the host the request path would see, not the host the
			// operator spelled: httpx.HostOnly is the one normalisation every
			// TenantLoader gets its lookup key through, so "ACME.Test",
			// "acme.test." and "acme.test:8080" are one host here exactly as they
			// are one host to the served router. Comparing the raw spellings instead
			// let two claims the process cannot tell apart start anyway.
			key := httpx.HostOnly(t.Host)
			if prior, taken := claims[key]; taken {
				spelled := t.Host
				if prior.spelled != t.Host {
					spelled = fmt.Sprintf("%s and %s are one host, %s", prior.spelled, t.Host, key)
				}
				errs = append(errs, fmt.Errorf("pkit: %s: Host: %s is claimed by both %s and %s; a host belongs to one tenant", h.app.name, spelled, prior.tenant, t.Name))
			}
			claims[key] = claim{tenant: t.Name, spelled: t.Host}
		}
	}
	if len(s.hosts) > 1 {
		errs = append(errs, fmt.Errorf("pkit: %s: Host: this process serves one app; %s would be the second on the same database. Many apps over one database wait for T-0231, which puts the app's name in every name two apps share and the check at every boundary (0074 rule 6)",
			s.hosts[0].app.name, andList(s.hostedNames())))
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Build answers every problem with the process and its hosted applications at
// once, and starts the one it can. The refusals are the process's own — nothing
// hosted, no deployment named, an application hosted twice, two tenants claiming
// one host, a second application on one database — and each is answered before
// anything is opened, in the same phase order App.Build uses.
func (s *Server) assemble(ctx context.Context) ([]*Runtime, []*Planned, error) {
	if err := s.refusals(); err != nil {
		return nil, nil, err
	}
	var rts []*Runtime
	var plans []*Planned
	for _, h := range s.hosts {
		rt, p, err := h.app.start(ctx, s.deploy, s.roleOrDefault(), s)
		if err != nil {
			for _, open := range rts {
				_ = open.Close()
			}
			return nil, nil, err
		}
		rts, plans = append(rts, rt), append(plans, p)
	}
	return rts, plans, nil
}

// Build is the process's whole validate-then-effect sequence: every hosted
// app's composition is resolved and answered, the process's own refusals are
// answered, and then — and only then — the engine migrates and opens.
func (s *Server) Build(ctx context.Context) (*Served, error) {
	rts, plans, err := s.assemble(ctx)
	if err != nil {
		return nil, err
	}
	return &Served{runtimes: rts, hosts: s.hosts, planned: plans}, nil
}

// Run is the process's answers and then whichever half the role names, until ctx
// is done. It asks the refusals rather than calling Build: kit/app's Run is its
// own Start plus the half the role names, so building an engine here and running a
// second one would leave a migrated database behind a process that never served.
func (s *Server) Run(ctx context.Context) error {
	if err := s.refusals(); err != nil {
		return err
	}
	// refusals answered for the index: it refuses a process that hosts nothing and
	// one that hosts two, so what is left is exactly the application this process
	// is.
	return s.hosts[0].app.runStarted(ctx, s.deploy, s.roleOrDefault(), s)
}

// Explain is the text of COMPOSITION.<env>.md: what the composition resolves to,
// which module answers each of the questions the kernel cannot answer for
// itself, and which host each tenant is claimed at. It is written for one named
// environment because there is no environment-free truth about a composition,
// and it carries no timestamp so the file a client commits can be diffed.
func (s *Server) Explain() (string, error) {
	if !s.set || len(s.hosts) == 0 {
		return "", errors.New("pkit: server: Explain needs a Deployment and one hosted app")
	}
	if len(s.hosts) > 1 {
		// The file is one application's composition, so a process hosting two would
		// be handed a document describing the first of them and silent about the
		// rest. Build refuses that process; the text it refuses is not a file a
		// reader can commit and diff, which is what this one is for.
		return "", fmt.Errorf("pkit: server: Explain writes one application's composition; this process hosts %s, which Build refuses as a second app on one database",
			andList(s.hostedNames()))
	}
	var b strings.Builder
	h := s.hosts[0]
	text, err := h.app.Explain(s.deploy)
	if err != nil {
		return "", err
	}
	p, err := h.app.Plan(s.deploy)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "# COMPOSITION — %s · %s\n\n", h.app.name, s.deploy.Environment)
	b.WriteString("Written by `pkit.Server.Explain` and the server's host claims. Do not edit:\n" +
		"apps/platformkit's TestTheCompositionFileIsCommittedForEachEnvironment refuses a\n" +
		"missing one, and `pkit.Server.Explain` is what writes the text.\n\n")
	fmt.Fprintf(&b, "App `%s` · environment `%s` · %d modules · role `%s`.\n",
		h.app.name, s.deploy.Environment, len(p.Modules()), s.roleOrDefault())
	b.WriteString("\n## Composition\n\n")
	b.WriteString(text)
	b.WriteString("\n## What the application answers the kernel\n\n")
	for _, port := range requiredPorts {
		key := port.key()
		fmt.Fprintf(&b, "- %s: %s\n", port.question, p.providedBy(key))
	}
	if ent, _ := Value[httpx.Entitler](p); ent != nil {
		fmt.Fprintf(&b, "- what their plan includes: %s\n", p.providedBy(reflect.TypeFor[httpx.Entitler]()))
	} else {
		b.WriteString("- what their plan includes: nothing, because no composed module declares a feature\n")
	}
	if h.app.refusal != nil {
		b.WriteString("- how a refusal looks: the application's own ErrorPage\n")
	} else {
		b.WriteString("- how a refusal looks: problem+json, which is what an API client wants\n")
	}
	if h.app.ask != nil {
		b.WriteString("- how a person asks for what was refused: the ask door and its page\n")
	}
	if s.deploy.Config.Server.InstallationHost != "" {
		fmt.Fprintf(&b, "- the installation itself, and the control plane: %s (from `server.installation_host`)\n",
			s.deploy.Config.Server.InstallationHost)
	}
	b.WriteString("\n## Tenant hosts\n\n")
	for _, t := range sortedHosts(h.tenants) {
		fmt.Fprintf(&b, "- %s — %s (claimed; the tenant rows decide who is served)\n", t.Name, t.Host)
	}
	if len(h.tenants) == 0 {
		b.WriteString("- none claimed: every tenant of this installation is reached at the address its own rows name\n")
	}
	return b.String(), nil
}

// start is Run's and Build's shared half: the engine this app resolves to under
// this process's role, configuration and transports. What the engine's effects
// cost the App is release's answer, the same one Build settles with: a process
// refused above the connection leaves its App free to build a corrected
// composition, and one that reached the pool does not.
func (a *App) start(ctx context.Context, d Deployment, role app.Role, s *Server) (*Runtime, *Planned, error) {
	engine, p, err := a.engineWith(ctx, d, role, s)
	if err != nil {
		return nil, nil, err
	}
	rt, err := engine.Start(ctx)
	if err != nil {
		a.release(err)
		return nil, nil, err
	}
	return &Runtime{rt: rt}, p, nil
}

// runStarted is Run's half: the engine this app resolves to, then whichever
// halves role names — kit/app's Run is its own Start and the serving, which is
// why the caller resolves an unset role to all before arriving here.
func (a *App) runStarted(ctx context.Context, d Deployment, role app.Role, s *Server) error {
	engine, _, err := a.engineWith(ctx, d, role, s)
	if err != nil {
		return err
	}
	err = engine.Run(ctx)
	a.release(err)
	return err
}

// engineWith is newEngine behind Server's door: the same claim, the same dry
// route gates before the first effect, and the same refusal, with this door's
// prefix on what the engine refused. It also returns the plan, because Server
// explains the composition it hosted and the plan is the only reading of it that
// names which module provided each contract.
func (a *App) engineWith(ctx context.Context, d Deployment, role app.Role, s *Server) (*app.App, *Planned, error) {
	engine, p, err := a.newEngine(ctx, d, role)
	if err != nil {
		return nil, nil, err
	}
	return engine, p, nil
}

// providerNames answers, for one contract, which composed module put it — the
// sentence a composition file needs and nothing else can print, because the
// provider is a fact about the resolved composition rather than the declared one.
func (p *Planned) providedBy(key any) string {
	switch who := p.putBy[key]; len(who) {
	case 1:
		return who[0].name + ".Module"
	case 0:
		return "nothing: no composed module put one"
	default:
		return andList(moduleNames(who)) + ", which is refused above"
	}
}

// sortedHosts is the tenant list in the order the file prints it: by tenant name,
// so the file a client commits does not move when a Host call is re-wrapped.
func sortedHosts(ts []TenantValue) []TenantValue {
	out := append([]TenantValue{}, ts...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
