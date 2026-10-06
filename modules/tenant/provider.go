package tenant

import (
	"context"
	"net"
	"strconv"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the control plane as the resolver sees it, for an application that
// names it in Use: `Use(tenant.Module)`.
//
// Four contracts come out of it, and three of them are what other modules
// already had to be handed across a composition file.
//
// Service is the control plane itself — kit/httpx resolves every request's host
// through it, before the request is about anything. jobs.TenantLister is the
// same service seen by a periodic sweep: five modules used to be handed
// `tenant.Active{Service: tenants}` by the composition, and the ceremony left
// when the module said what it hands out. HostLookup answers a mailed link's
// host, so one customer's people are never sent to another's front door.
// authcontracts.OIDCProviders answers which issuer the tenant this request's
// Host resolved signs in against, per request, from the transaction the header
// chose.
//
// What it cannot decide is who seeds a new tenant (every composed hook, in
// order), who its first administrator is (the invitation port, which the user
// module answers) and which languages a locale may be set to (the composition
// read the files; this module names no tag).
var Module = pkit.NewModule("tenant", wire,
	pkit.Needs[tenantcontracts.Inviter](),
	pkit.Needs[[]tenantcontracts.Hook](),
	pkit.Optional[tenantcontracts.Languages](),
	pkit.Provides[tenantcontracts.Service](),
	// Which host is which tenant is this module's rows and nobody else's: the
	// kernel asks the question before a request is about anything, and the
	// answer used to be put by a module apps/platformkit wrote for the purpose.
	pkit.Provides[httpx.TenantLoader](),
	pkit.Provides[jobs.TenantLister](),
	pkit.Provides[notificationcontracts.HostLookup](),
	pkit.Provides[authcontracts.OIDCProviders](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	nats := pkit.Config(w, func(c config.Config) config.NATS { return c.NATS })
	server := pkit.Config(w, func(c config.Config) config.Server { return c.Server })
	// The app these tenants belong to. One setting names it for the whole
	// composition — the same value the event addresses, the durable, the job
	// lock and the cookie name are formed from — and empty is this application
	// as it has always been: the deployment of one app.
	//
	// An unparseable slug refuses the build rather than panicking it, which is
	// what the composition's appSlug used to do: the same refusal, one sentence
	// earlier and naming the setting instead of the stack.
	slug, err := nats.AppName()
	if err != nil {
		return module.Module{}, err
	}
	svc, manifest := New(Deps{
		OnCreate:  pkit.All[tenantcontracts.Hook](w),
		Invite:    pkit.Get[tenantcontracts.Inviter](w),
		Languages: pkit.Get[tenantcontracts.Languages](w).Tags,
		App:       slug,
	})
	pkit.Put(w, svc)
	pkit.Put[httpx.TenantLoader](w, svc)
	pkit.Put[jobs.TenantLister](w, tenantcontracts.Active{Service: svc})
	hosts := tenantHosts{tenants: svc, published: publishedPort(server.PublicHost)}
	pkit.Put[notificationcontracts.HostLookup](w, hosts)
	pkit.Put[authcontracts.OIDCProviders](w, tenantProviders{tenants: svc})
	return manifest, nil
}

// tenantHosts is the adapter that lets the notification and auth modules build a
// link to the recipient's own host without naming the tenant module.
//
// The lookup runs in the worker's own tenant transaction, so what it reads is
// the one tenant's rows the policy shows it. A tenant with several hosts answers
// at all of them and a message has to pick one, so it picks the primary — the
// first row of a list ordered by it (migrations/000020). It used to pick
// whichever name sorted first, which meant adding admin.acme.example.com moved
// every future link onto it.
//
// published is the port the installation is reached at, read once out of
// server.public_host, and "" when that address names none: see publishedPort.
type tenantHosts struct {
	tenants   tenantcontracts.Service
	published string
}

func (h tenantHosts) PublicHost(ctx context.Context, tx db.Tx[db.Tenant]) (string, error) {
	hosts, err := h.tenants.Hosts(ctx, tx)
	if err != nil || len(hosts) == 0 {
		return "", err
	}
	// A host of record is a name and nothing else: this module refuses a port in
	// one, because kit/httpx resolves a request by matching the name in its Host
	// header against this column. So the port a published installation is
	// reached at cannot live in the row, and the row alone is the whole of an
	// address only when the installation answers at its scheme's default port.
	//
	// An installation published behind a mapping — a container run -p 38591:8080,
	// a compose `ports:` entry, a NodePort — answers the browser at one port and
	// its own socket at another, and the socket cannot tell anybody the first.
	// server.public_host is where the installation says it, and this is the one
	// line that carries it onto every link this application mails, since both of
	// the builders (auth's reset link and notification's notice link) make their
	// URL out of what is returned here. A link with no declared port keeps
	// falling back to the port the request was answered on, which
	// modules/auth/internal/served.go appends; a host that already spells a port
	// is left exactly as it is spelled, which is the rule that fallback already
	// honours, so the two never disagree.
	if h.published != "" {
		if _, _, err := net.SplitHostPort(hosts[0]); err != nil {
			return hosts[0] + ":" + h.published, nil
		}
	}
	return hosts[0], nil
}

// publishedPort is the port the address in declared says its installation is
// reached at, and "" when it names none or does not read as an authority at all.
//
// It is a port and not a name: which name a link carries is the tenant's row's
// answer, and one customer's people are never sent to another's front door by a
// configuration value. kit/config already refuses anything that is not a host
// with an optional port here, so the empty answer means "not declared", which is
// every deployment that answers at 443 or at a port the browser does not write.
func publishedPort(declared string) string {
	name, port, err := net.SplitHostPort(declared)
	if err != nil || name == "" {
		return ""
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return ""
	}
	return port
}

// tenantProviders answers modules/auth's OIDCProviders port from the tenant this
// request's Host resolved. The transaction is the request's own, which is what
// makes the answer per tenant and per request rather than per process.
//
// It is here rather than in the composition for the same reason the host lookup
// is: the read is this module's own — OIDCOf is a method on its Service — and
// auth names only the port it declares.
type tenantProviders struct{ tenants tenantcontracts.Service }

func (p tenantProviders) ProviderOf(ctx context.Context, tx db.Tx[db.Tenant]) (*authcontracts.OIDCProvider, bool, error) {
	settings, ok, err := p.tenants.OIDCOf(ctx, tx)
	if err != nil || !ok {
		return nil, false, err
	}
	return &authcontracts.OIDCProvider{
		Issuer: settings.Issuer, ClientID: settings.ClientID, SecretRef: settings.SecretRef,
		RedirectPath: settings.RedirectPath, Registration: settings.Registration, Roles: settings.Roles,
	}, true, nil
}
