package httpx

// tenant.go resolves the request's host to a tenant, with a short-lived cache
// whose entry an installation can invalidate the moment a host changes hands.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// hostTTL is how long a resolved host is believed. Long enough that a busy site
// costs one query per half minute, short enough that adding a domain is a
// coffee rather than a deploy.
const hostTTL = 30 * time.Second

type hostEntry struct {
	tenant tenancy.Tenant
	until  time.Time
}

// hostCache remembers resolutions that succeeded, and only those. Caching a
// failure would turn one blink of the database into thirty seconds of refusals
// for that host, and would let anyone fill the map with Host headers they
// invented; remembering only real tenants bounds it by data the operator owns.
type hostCache struct {
	mu    sync.Mutex
	hosts map[string]hostEntry
	// invalidations counts the drops this cache has applied. One count for the
	// whole cache rather than one per tenant: an outstanding lookup does not know
	// which tenant it is about to return, so the count it stamps against is only
	// readable before the question. A lookup that spans another tenant's drop then
	// costs one more query, which is all it costs.
	invalidations uint64
}

func (c *hostCache) get(host string) (tenancy.Tenant, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.hosts[host]
	if !ok {
		return tenancy.Tenant{}, false
	}
	if time.Now().After(e.until) {
		// Dropped on the way past, so a host that stopped being served stops
		// occupying the map instead of waiting for a restart.
		delete(c.hosts, host)
		return tenancy.Tenant{}, false
	}
	return e.tenant, true
}

// stamp reads the drop count. A resolution stamps with it before it asks, and
// publishes against it; see put.
func (c *hostCache) stamp() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.invalidations
}

// remove forgets one name, and is a drop of it: a host that changed hands while
// a lookup of it was outstanding must not answer as a tenant no longer served here.
func (c *hostCache) remove(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.invalidations++
	delete(c.hosts, host)
}

// dropTenant forgets every name that resolves to one tenant.
//
// A tenant's languages and its status travel inside a cached resolution, and the
// names a tenant answers at are the ones that carry it: forgetting one host of a
// tenant that has four leaves three pages still served in the languages somebody
// just replaced. The scan is over the resolutions this installation holds, which
// is bounded by the hosts the operator owns.
func (c *hostCache) dropTenant(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for host, e := range c.hosts {
		if e.tenant.ID == id {
			delete(c.hosts, host)
		}
	}
	c.invalidations++
}

// put publishes a resolution, unless this tenant, or this host, was dropped while
// the lookup was outstanding.
//
// A resolution reads a snapshot, and a commit that landed after the snapshot does
// not travel back into it. The request that asked is answered from that snapshot —
// it began before the write, and a page in the languages as they were when its
// request started is not a page that is wrong — but the requests after that write
// must not be. Stored, this resolution would answer them for hostTTL, because the
// drop at the commit had nothing to drop: a name whose lookup is still in flight
// has no entry yet. That is the window ForgetTenantOnCommit exists to close.
func (c *hostCache) put(host string, t tenancy.Tenant, stamped uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.invalidations != stamped {
		return
	}
	c.hosts[host] = hostEntry{tenant: t, until: time.Now().Add(hostTTL)}
}

// resolveTimeout bounds the one query every request makes before it is a
// request. Without it a database that accepts connections and answers nothing
// holds every arriving request open on the client's patience rather than ours.
const resolveTimeout = 2 * time.Second

// InvalidateHost forgets a cached resolution, so a rename or a removal takes
// effect now rather than within hostTTL. The tenant module calls it when it
// changes a host; nothing else has any reason to.
func (a *API) InvalidateHost(host string) { a.hosts.remove(HostOnly(host)) }

// forgottenTenants is the list one request accumulates while it runs, and the
// reason it is a pointer behind a context value: the middleware that commits the
// transaction is above the handler that decided to change a tenant, and it has to
// read what that handler asked for.
type forgottenTenants struct {
	mu      sync.Mutex
	tenants []uuid.UUID
}

func (f *forgottenTenants) add(id uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tenants = append(f.tenants, id)
}

// drain takes the list, leaving it empty: a request is answered once, and what it
// asked for is forgotten once.
func (f *forgottenTenants) drain() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	asked := f.tenants
	f.tenants = nil
	return asked
}

// ForgetTenantOnCommit asks the kernel to drop every cached resolution of one
// tenant once this request's transaction commits — and to forget nothing if that
// commit does not happen.
//
// The cached resolution carries the tenant's languages, so a command that changed
// them leaves every page answered in the old set until the entry expires, half a
// minute of pages that contradict the row the caller just saved. A writer on a
// system transaction that has already committed invalidates directly instead
// (InvalidateHost); this is for the writer inside the request's own transaction,
// which cannot see its own commit and must not pretend it already happened.
//
// The list is held until the commit for the reason a premature invalidation is no
// invalidation at all: a request that arrives while this one is still open, and
// resolves the host again, puts the old resolution back for another half minute.
//
// A call outside a request transaction is a no-op, and that is the whole answer
// rather than a swallowed failure: the write whose result could go stale needs
// the same transaction this list is carried by, so a caller with no transaction
// has written nothing and has nothing to forget. A writer that committed a change
// on a system transaction invalidates through the router instead.
func ForgetTenantOnCommit(ctx context.Context, tenant uuid.UUID) {
	if tenant == uuid.Nil {
		return
	}
	if f, ok := ctx.Value(forgetKey{}).(*forgottenTenants); ok {
		f.add(tenant)
	}
}

// resolve maps a host to a tenant, through the loader, inside a cross-tenant
// transaction the kernel opens for it. The loader is a module: it cannot mint
// the capability itself, and it never holds one outside this call.
//
// Concurrent misses for one host share a single query. A cold cache at the
// front of a traffic spike is otherwise one lookup per request, all of them
// asking the same question.
//
// The shared lookup carries the drop count it stamped itself with before it asked,
// so a lookup that overlaps a committed change to its tenant serves the request
// that waited on it and stops there: see hostCache.put.
func (a *API) resolve(ctx context.Context, host string) (tenancy.Tenant, error) {
	if t, ok := a.hosts.get(host); ok {
		return t, nil
	}
	shared, err, _ := a.resolving.Do(host, func() (any, error) {
		stamped := a.hosts.stamp()
		if t, ok := a.hosts.get(host); ok {
			return hostResolution{tenant: t, stamped: stamped}, nil
		}
		// WithoutCancel, because this lookup is shared: the request that
		// happened to arrive first must not take everyone else's answer with
		// it when it gives up. The timeout is what bounds it instead.
		qctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), resolveTimeout)
		defer cancel()
		var t tenancy.Tenant
		err := db.RunSystem(qctx, a.opts.Conn, a.token, func(rctx context.Context, tx db.Tx[db.System]) error {
			var err error
			t, err = a.opts.Tenants.ByHost(rctx, tx, host)
			return err
		})
		return hostResolution{tenant: t, stamped: stamped}, err
	})
	if err != nil {
		return tenancy.Tenant{}, err
	}
	resolution := shared.(hostResolution)
	t := resolution.tenant
	if t.ID == uuid.Nil {
		// A loader that answers with the zero Tenant and no error has resolved
		// nothing and does not know it. Taking it at its word would scope the
		// request's transaction to the nil UUID and, worse, make every zero
		// Principal a member of it.
		return tenancy.Tenant{}, fmt.Errorf("the loader returned the zero tenant for %q", host)
	}
	a.hosts.put(host, t, resolution.stamped)
	return t, nil
}

// hostResolution is one answer of the shared lookup, with the drop count the
// lookup held before it asked. The answer goes to every request that joined the
// lookup; the stamp decides whether it may also be remembered.
type hostResolution struct {
	tenant  tenancy.Tenant
	stamped uint64
}

// tenant resolves the request host to a tenant and puts it on the context,
// where kit/db reads it.
//
// A host that names no tenant is a 404 rather than a 400: from outside, a site
// that is not served and a site that does not exist are the same fact. A loader
// that could not tell is a 503, because an outage that reads as "this site does
// not exist" is how a deployment gets debugged in the wrong direction. The one
// exception is an operation that declared itself Public, which may legitimately
// be reached at a host the loader knows nothing about — a health probe
// addressing the pod by IP, say — and then proceeds with no tenant and, below,
// no transaction.
func (a *API) tenant(ctx huma.Context, next func(huma.Context)) {
	host := HostOnly(ctx.Host())
	t, err := a.resolve(ctx.Context(), host)
	if err == nil {
		next(huma.WithContext(ctx, tenancy.WithTenant(ctx.Context(), t)))
		return
	}
	unknown := errors.Is(err, tenancy.ErrNoSuchHost)
	if unknown {
		a.rlog(ctx.Context()).DebugContext(ctx.Context(), "httpx: no site at host", "host", host)
	} else {
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: could not resolve the host to a tenant",
			"host", host, "error", err)
	}
	if auth, ok := declarationOf(ctx.Operation()); ok && auth.kind == kindPublic {
		next(ctx)
		return
	}
	if unknown {
		a.refuse(ctx, http.StatusNotFound, "no site is served at "+host)
		return
	}
	ctx.SetHeader("Retry-After", "3")
	a.refuse(ctx, http.StatusServiceUnavailable, "this host cannot be resolved right now")
}

// withHostTenant hands a refusal the request that names its tenant.
//
// Host resolution is an operation middleware: it runs once a route has been chosen.
// The refusals this package answers for itself — the address nobody mounted, the verb
// an address does not take, the cross-site write, the handler that panicked — are
// answered ahead of any route, on a request that therefore carries no tenant, and the
// page one of them renders would then negotiate its language from the caller's
// Accept-Language alone. Which languages a request is answered in is a fact about the
// tenant behind its host and not about the header (ui/page.Served), so a guard that
// answers in a language this tenant never declared answers in one it refused.
//
// A refusal is allowed to ask the question the route never got to ask. The answer is
// already in the cache every request warms, the one query it may cost is bounded by
// resolveTimeout, and a request whose context is already over gets no query at all.
// A resolution that fails leaves the request as it arrived, which is the case the
// deployment's own catalogue answers for: a host nobody serves has no tenant to
// declare a language, and a database that is not answering has larger problems than
// the language of a refusal page.
//
// The guards inside the huma chain need no such call: they run after a.tenant, which
// either put a tenant on the request or refused the host for having none.
func (a *API) withHostTenant(r *http.Request) *http.Request {
	ctx := r.Context()
	if _, resolved := tenancy.FromContext(ctx); resolved || ctx.Err() != nil {
		return r
	}
	t, err := a.resolve(ctx, HostOnly(r.Host))
	if err != nil {
		return r
	}
	return r.WithContext(tenancy.WithTenant(ctx, t))
}

// HostOnly is the loader's key: the Host header without its port and without
// the brackets an IPv6 literal carries, lower-cased, without the trailing dot a
// fully qualified name may have. Normalising here means every TenantLoader is
// spared doing it, and doing it differently.
//
// It is exported because the module that stores a host has to spell it the same
// way as the middleware that looks one up, and two normalisations that drift is
// a domain that resolves for nobody.
func HostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	return strings.TrimSuffix(strings.ToLower(host), ".")
}
