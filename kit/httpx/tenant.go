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
	"github.com/septagon-oss/platformkit/kit/telemetry"
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

func (c *hostCache) remove(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.hosts, host)
}

func (c *hostCache) put(host string, t tenancy.Tenant) {
	c.mu.Lock()
	defer c.mu.Unlock()
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

// resolve maps a host to a tenant, through the loader, inside a cross-tenant
// transaction the kernel opens for it. The loader is a module: it cannot mint
// the capability itself, and it never holds one outside this call.
//
// Concurrent misses for one host share a single query. A cold cache at the
// front of a traffic spike is otherwise one lookup per request, all of them
// asking the same question.
func (a *API) resolve(ctx context.Context, host string) (tenancy.Tenant, error) {
	if t, ok := a.hosts.get(host); ok {
		return t, nil
	}
	shared, err, _ := a.resolving.Do(host, func() (any, error) {
		if t, ok := a.hosts.get(host); ok {
			return t, nil
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
		return t, err
	})
	if err != nil {
		return tenancy.Tenant{}, err
	}
	t := shared.(tenancy.Tenant)
	if t.ID == uuid.Nil {
		// A loader that answers with the zero Tenant and no error has resolved
		// nothing and does not know it. Taking it at its word would scope the
		// request's transaction to the nil UUID and, worse, make every zero
		// Principal a member of it.
		return tenancy.Tenant{}, fmt.Errorf("the loader returned the zero tenant for %q", host)
	}
	a.hosts.put(host, t)
	return t, nil
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
		// Both keys, because they answer two questions a reader asks at different
		// moments: the slug is what a person recognises in a trace, the id is what
		// joins this span to the delivery spans of the events this request published
		// and to the same tenant's share of a job. A host the loader does not know
		// reaches neither line below, and gets neither key — see traced.go.
		spanAttr(ctx.Context(), telemetry.AttrTenant, t.Slug)
		spanAttr(ctx.Context(), telemetry.AttrTenantID, t.ID.String())
		// And to the response's note, so the refusal count respond writes after the
		// chain returns can name the tenant this request resolved: an operator
		// filtering the number by tenant has to be able to ask it of a refusal too,
		// not only of a latency bar. See answerNote.
		noteAnswer(ctx.Context(), t)
		// The latency number is recorded here and not in the operation middleware
		// that names the span, because this is the only place that holds both the
		// operation and the tenant the host resolved to. A bar that cannot be split
		// by tenant is the number this runtime promises not to publish: one process,
		// many tenants, and an aggregate that cannot say whose requests are slow
		// answers the question nobody asked.
		start := time.Now()
		tctx := tenancy.WithTenant(ctx.Context(), t)
		next(huma.WithContext(ctx, tctx))
		observeOperation(tctx, ctx.Operation(), time.Since(start).Seconds())
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
