package httpx

// tenant.go resolves the request's host to a tenant, with a short-lived cache
// whose entry an installation can invalidate the moment a host changes hands.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// hostTTL is how long a resolved host is believed. Long enough that a busy site
// costs one query per half minute, short enough that adding a domain is a
// coffee rather than a deploy.
const hostTTL = 30 * time.Second

// hostScope is the namespace of host resolutions, and it is Shared rather than
// Of(tenant): a host resolution is the thing that *decides* a tenant, so it belongs
// to the installation and to no customer. It is a package constant and not caller
// data, because the invalidation and the write must name one namespace.
var hostScope = cache.Shared("host")

// cachedTenant is the part of a resolution a request needs before it has a
// transaction — which is to say, all of it. It is a wire struct rather than
// encoding tenancy.Tenant itself for two reasons: the shape on the store is then a
// declaration this package owns and can pin (TestACachedResolutionCarriesTheWhole-
// Tenant), and a field added to the kernel's Tenant tomorrow does not arrive in the
// cache by invisibility. Dropping a field here is a real bug and must look like one:
// Operator decides whether a caller may reach the control plane, and a resolution
// that came back without it would refuse an operator whenever the cache answered.
type cachedTenant struct {
	ID        uuid.UUID          `json:"id"`
	Slug      string             `json:"slug"`
	Name      string             `json:"name"`
	Operator  bool               `json:"operator"`
	Languages *tenancy.Languages `json:"languages,omitempty"`
}

func (c cachedTenant) tenant() tenancy.Tenant {
	return tenancy.Tenant{ID: c.ID, Slug: c.Slug, Name: c.Name, Operator: c.Operator, Languages: c.Languages}
}

func newCachedTenant(t tenancy.Tenant) cachedTenant {
	return cachedTenant{ID: t.ID, Slug: t.Slug, Name: t.Name, Operator: t.Operator, Languages: t.Languages}
}

// resolveTimeout bounds the one query every request makes before it is a
// request. Without it a database that accepts connections and answers nothing
// holds every arriving request open on the client's patience rather than ours.
const resolveTimeout = 2 * time.Second

// InvalidateHost closes the namespace of host resolutions, so a rename, a
// suspension or a new language declaration takes effect now rather than within
// hostTTL. The tenant module calls it when it changes a host; nothing else has any
// reason to.
//
// It is a Move and not a Delete of the hosts named, and the race is the reason for
// the whole of kit/cache. A delete forgets what is in the store at the moment it
// runs and nothing else: replica A has missed, is already in its loader, and the
// operator's replica commits a suspension and deletes the key; replica A writes the
// tenant it loaded a moment later, and every replica that reads this store serves a
// suspended host for the rest of hostTTL. A move forbids what arrives afterwards as
// well as what is there — TestAnInvalidationDuringALoadLeavesNoResolutionBehind is
// that interleaving run through this function rather than through the port. The
// names are therefore not the keys being forgotten, and nothing here normalises
// them: the namespace is what closes.
//
// What the names decide is whether anything is closed at all: a change that touched
// no host changes no resolution, and a tenant with no hosts costs no command.
// The cost of the coarser invalidation is that one suspension costs every other host
// one loader query on its next request — for a handful of operator actions a day
// over an indexed query, the trade kit/cache names on Move.
//
// It returns the store's error, because the invalidation crosses a network and a
// failure there is a fact the caller has to answer for: the committed write stands —
// unwinding a suspension because a cache stopped answering is the worse outage — but
// the change is not yet true at every process reading that store, and the route that
// made it says so rather than reporting an outcome it did not achieve.
func (a *API) InvalidateHost(hosts ...string) error {
	if len(hosts) == 0 {
		return nil
	}
	return a.opts.Cache.Move(context.Background(), hostScope)
}

// resolve maps a host to a tenant, through the shared store and, on a miss, the
// loader inside a cross-tenant transaction the kernel opens for it. The loader is a
// module: it cannot mint the capability itself, and it never holds one outside this
// call.
//
// Concurrent misses for one host share a single query. A cold cache at the front of
// a traffic spike is otherwise one lookup per request, all of them asking the same
// question — and now the answer any one of them writes is the answer every other
// replica reads, which is the half a map in this process could not do.
func (a *API) resolve(ctx context.Context, host string) (tenancy.Tenant, error) {
	if a.opts.Conn == nil {
		// An unwired API nobody connected. Every answer about a host has to come
		// from the deployment's own database, so the honest answer is the error
		// every caller of resolve already turns into a refusal to serve this host
		// — not a nil-pointer dereference inside db.
		return tenancy.Tenant{}, errors.New("httpx: this API was built unwired and was never connected to a database")
	}
	key := hostScope.Entry(host)
	if t, ok := a.cached(ctx, key); ok {
		return t, nil
	}
	shared, err, _ := a.resolving.Do(host, func() (any, error) {
		resolved, hit, under := a.cachedUnder(ctx, key)
		if hit {
			return resolved, nil
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
		if err != nil {
			return tenancy.Tenant{}, err
		}
		if t.ID == uuid.Nil {
			// A loader that answers with the zero Tenant and no error has resolved
			// nothing and does not know it. Taking it at its word would scope the
			// request's transaction to the nil UUID and, worse, make every zero
			// Principal a member of it. It is refused here, before the store sees it,
			// so no path — this load or a later read of an entry — can put the nil
			// tenant on a request.
			return tenancy.Tenant{}, fmt.Errorf("the loader returned the zero tenant for %q", host)
		}
		// Successes only. Caching a failure would turn one blink of the database into
		// half a minute of refusals for that host, and would let anyone fill the store
		// with Host headers they invented; remembering only real tenants bounds it by
		// data the operator owns.
		buf, err := json.Marshal(newCachedTenant(t))
		if err != nil {
			// A tenant this package cannot serialise is a tenant this store has no
			// business holding; the request is served from the loader anyway.
			a.log.DebugContext(ctx, "httpx: could not put a host resolution in the shared store", "host", host, "error", err)
			return t, nil
		}
		// The write carries the generation the miss above read open, so a move that
		// lands during this query closes the entry it would have written: the answer
		// the installation stopped believing serves the request that was already
		// loading and is not believed for hostTTL by every replica that reads this
		// store. One Set per shared load, not one per request that waited for it.
		if err := a.opts.Cache.Set(ctx, key, buf, hostTTL, under); err != nil {
			a.log.WarnContext(ctx, "httpx: could not store a host resolution; this replica will ask again",
				"host", host, "error", err)
		}
		return t, nil
	})
	if err != nil {
		return tenancy.Tenant{}, err
	}
	t := shared.(tenancy.Tenant)
	return t, nil
}

// cached reads the store and takes the answer at its word. See cachedUnder, which
// is the same read with the half a caller needs to write.
func (a *API) cached(ctx context.Context, key cache.Key) (tenancy.Tenant, bool) {
	t, ok, _ := a.cachedUnder(ctx, key)
	return t, ok
}

// cachedUnder reads the store and answers the generation its read found open,
// which is what a write that follows this miss has to be stamped with.
//
// A miss, a value that will not decode, a resolution of the nil tenant and an
// unreachable store are all "ask the loader": the database is the truth for a
// resolution, so failing open to it is always correct and costs one query. The
// distinction the log keeps is the one an operator needs — a store that is
// answering nothing is an outage, and a cold cache is not.
func (a *API) cachedUnder(ctx context.Context, key cache.Key) (tenancy.Tenant, bool, cache.Generation) {
	buf, found, under, err := a.opts.Cache.Get(ctx, key)
	if err != nil {
		a.log.WarnContext(ctx, "httpx: the shared cache is not answering; resolving from the database",
			"host", key.Entry(), "error", err)
		return tenancy.Tenant{}, false, cache.Generation{}
	}
	if !found {
		return tenancy.Tenant{}, false, under
	}
	var c cachedTenant
	if err := json.Unmarshal(buf, &c); err != nil {
		return tenancy.Tenant{}, false, under
	}
	if c.ID == uuid.Nil {
		return tenancy.Tenant{}, false, under
	}
	return c.tenant(), true, under
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
		//
		// Deferred, so the bar belongs to every answer this request got and not to the
		// answers that came back up a call stack: a panicking handler unwinds past a
		// statement placed after next, and respond — below this middleware — is what
		// turns that panic into the 500 the client was given. The histogram is the
		// denominator of every latency question, so the shape of an answer cannot
		// decide whether its seconds are in it: if only the refusal counter notices a
		// request that fell over, an operator reads "refusals up, latency flat" and
		// triages the wrong thing at the moment it costs most.
		start := time.Now()
		tctx := tenancy.WithTenant(ctx.Context(), t)
		defer func() {
			observeOperation(tctx, ctx.Operation(), time.Since(start).Seconds())
		}()
		next(huma.WithContext(ctx, tctx))
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
