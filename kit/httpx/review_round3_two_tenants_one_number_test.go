package httpx_test

// Review round 3 (T-0110). This is the case the delivery's own number is about.
//
// e9fc2f9 states tenant_attributed_boundary_coverage as 80/81, and the package
// README states why a tenant belongs on every datapoint: "one process, many
// tenants, and an aggregate that cannot be split by tenant cannot tell you which
// customer is having the bad afternoon". Every existing case in this package
// resolves exactly ONE tenant — tracedFixture answers "acme.test" and refuses
// every other host — so what is pinned today is "the number names acme". The
// failure that is invisible to that assertion is the one worth a case: a tenant
// attributed to the wrong request, or two tenants' requests smeared into one time
// series, still names *a* tenant.
//
// The mechanism that could smear is real and is this delivery's own. respond
// allocates a per-request answerNote; the middleware that resolves the tenant
// writes it (tenant.go, noteAnswer); and the count happens after the chain
// returns, in a defer, in a middleware holding a request context that predates the
// resolution (respond.go, answerNote.context). One note shared between two
// requests — or one note that outlived its own request — and tenant B's refusals
// would be counted against tenant A, which is a number an operator would act on
// for the wrong customer.
//
// So the case runs two tenants through one router in one process, in the order a
// shared-instance deployment meets them (served, refused by a guard, refused by a
// guard, served, plus two refusals the root router answers), and asks of every
// number and every span whose request it was. Each step reaches its assertion
// through the status the router answered and the request id the response header
// carries, so no change to a refusal's wording or its class can hide the case from
// itself.
//
// What this case deliberately does not assert: that a 404 or a 405 the root router
// answers at a host it *could* resolve carries a tenant. Today it carries none —
// nothing on that path resolved one, which is the rule the README states ("a key is
// written only where its value was learned"). REVIEW.md records as a deferred
// finding that an operator therefore cannot ask "whose 404s are these" at all, and
// pins nothing that would resist a change either way.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// secondHost is a second customer's address on the same router, in the same
// process, at the same instant the first one is being served.
const secondHost = "other.test"

var (
	acmeID = uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	other  = uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
)

// deniedPermission is the guard this fixture refuses with, so the 403 below is a
// security refusal the operator must be able to filter by tenant.
const deniedPermission = "probe:read"

// twoTenantRouter is the shared instance: one mux, one mounted API, two
// operations, and a loader that resolves two hosts to two tenants.
func twoTenantRouter(t *testing.T) http.Handler {
	t.Helper()
	_, app := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host,
		Conn:       app,
		Cache:      cache.Memory("pkit"),
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
			switch h {
			case host:
				return tenancy.Tenant{ID: acmeID, Slug: "acme", Name: "Acme"}, nil
			case secondHost:
				return tenancy.Tenant{ID: other, Slug: "other", Name: "Other"}, nil
			}
			return tenancy.Tenant{}, tenancy.ErrNoSuchHost
		}),
		Authorize: authorizerFunc(func(_ context.Context, _ tenancy.Tenant, g tenancy.Grant) (bool, error) {
			return g.Permission != deniedPermission, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "round3_ping", Method: http.MethodGet, Path: "/ping",
	}, httpx.Public(), func(context.Context, *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	})
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "round3_secret", Method: http.MethodGet, Path: "/secret",
	}, httpx.Permission(deniedPermission), func(context.Context, *struct{}) (*struct{}, error) {
		t.Errorf("the guard let a caller through to round3_secret, so the 403 below is not a refusal")
		return &struct{}{}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the routes do not declare themselves: %v", err)
	}
	return router
}

// twoTenants is the pair, named once so the loop and the assertions cannot drift.
var twoTenants = map[string]uuid.UUID{"acme": acmeID, "other": other}

// TestEachTenantsNumberNamesOnlyItsOwnTenant runs six requests over one router and
// requires each to move exactly one tenant's number, by exactly one.
func TestEachTenantsNumberNamesOnlyItsOwnTenant(t *testing.T) {
	router := twoTenantRouter(t)

	lat := func(tenant uuid.UUID) uint64 { return histCount(t, "round3_ping", tenant) }
	forbidden := func(tenant uuid.UUID) int64 { return refusalSum(t, "forbidden", &tenant) }
	notFound := func() int64 { return refusalClassTotal(t, "not_found") }

	beforeLat := map[uuid.UUID]uint64{acmeID: lat(acmeID), other: lat(other)}
	beforeForbidden := map[uuid.UUID]int64{acmeID: forbidden(acmeID), other: forbidden(other)}
	beforeNotFound := notFound()

	for step, r := range []struct {
		host string
		path string
		want int // the status the router must answer for this step to be the case it claims
	}{
		{host, "/api/v1/probe/ping", http.StatusNoContent},
		{host, "/api/v1/probe/secret", http.StatusForbidden},
		{secondHost, "/api/v1/probe/secret", http.StatusForbidden},
		{secondHost, "/api/v1/probe/ping", http.StatusNoContent},
		{host, "/api/v1/probe/nomount", http.StatusNotFound},
		{secondHost, "/api/v1/probe/nomount", http.StatusNotFound},
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+r.host+r.path, nil))
		// Reachability, off the response the client got: the status says this is the
		// kind of answer the step counts, and an id in the header says the kernel
		// answered it. Nothing here reads a counter to decide whether to look at it.
		if rec.Code != r.want {
			t.Fatalf("request %d (%s%s) answered %d, want %d: %s", step, r.host, r.path, rec.Code, r.want, rec.Body.String())
		}
		if rec.Header().Get(httpx.RequestIDHeader) == "" {
			t.Fatalf("request %d carries no %s, so nothing below would prove these requests were separate",
				step, httpx.RequestIDHeader)
		}
	}

	// Each served request moved its own tenant's latency bar by one and the other
	// tenant's not at all. A bar attributed to nobody, or to the wrong customer, is
	// the number the delivery promised this runtime would not publish.
	for tenant, id := range twoTenants {
		if got := lat(id) - beforeLat[id]; got != 1 {
			t.Errorf("tenant %q: %s moved by %d for one served request, want exactly 1", tenant, "round3_ping", got)
		}
	}
	// Each guard refusal moved its own tenant's count of class forbidden by one and
	// the other tenant's not at all. This is the half a shared answerNote would get
	// wrong, and it is the security number: whose callers were refused.
	for tenant, id := range twoTenants {
		if got := forbidden(id) - beforeForbidden[id]; got != 1 {
			t.Errorf("tenant %q: refusals of class forbidden moved by %d for one refused request, want exactly 1 "+
				"(the refusal of one tenant's caller counted against another is a number an operator would act on "+
				"for the wrong customer)", tenant, got)
		}
	}
	// The two refusals the root router wrote are counted, once each, whatever they
	// are attributed to: counted twice would be the review-round-1 defect back, and
	// counted zero would be a refusal nobody sees.
	if got := notFound() - beforeNotFound; got != 2 {
		t.Errorf("refusals of class not_found moved by %d across two requests that each matched nothing, want exactly 2", got)
	}

	// And no datapoint anywhere pairs one tenant's id with the other tenant's slug:
	// that is the smear a shared note leaves, and it is what this case exists to see.
	for _, p := range allPointAttrs(t) {
		id, hasID := p.Value(attribute.Key(telemetry.AttrTenantID))
		slug, hasSlug := p.Value(attribute.Key(telemetry.AttrTenant))
		if !hasID || !hasSlug {
			continue
		}
		switch {
		case id.AsString() == acmeID.String() && slug.AsString() != "acme":
			t.Errorf("a datapoint names tenant %s with slug %q: %v", id.AsString(), slug.AsString(), p.ToSlice())
		case id.AsString() == other.String() && slug.AsString() != "other":
			t.Errorf("a datapoint names tenant %s with slug %q: %v", id.AsString(), slug.AsString(), p.ToSlice())
		}
	}
}

// TestEachTenantsSpanNamesOnlyItsOwnTenant is the same pair on the other half of
// the promise: a span describes one request, so the second tenant's span carries
// the second tenant, its own request id, and nothing of the first.
func TestEachTenantsSpanNamesOnlyItsOwnTenant(t *testing.T) {
	router := twoTenantRouter(t)
	for _, r := range []struct {
		host string
		slug string
		id   uuid.UUID
	}{
		{host, "acme", acmeID},
		{secondHost, "other", other},
	} {
		spans.Reset()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+r.host+"/api/v1/probe/ping", nil))
		if rec.Code >= http.StatusBadRequest {
			t.Fatalf("%s answered %d, want the handler reached: %s", r.host, rec.Code, rec.Body.String())
		}
		span := serverSpan(t)
		if got, found := attributeOf(span, telemetry.AttrTenant); !found || got.Emit() != r.slug {
			t.Errorf("%s: span %s carries pkit.tenant %v (found %v), want %q", r.host, span.Name(), got, found, r.slug)
		}
		if got, found := attributeOf(span, telemetry.AttrTenantID); !found || got.Emit() != r.id.String() {
			t.Errorf("%s: span %s carries pkit.tenant.id %v (found %v), want %s", r.host, span.Name(), got, found, r.id)
		}
		if id, found := attributeOf(span, telemetry.AttrRequestID); !found || id.Emit() != rec.Header().Get(httpx.RequestIDHeader) {
			t.Errorf("%s: span carries pkit.request.id %v (found %v), want the id this response returned %q",
				r.host, id, found, rec.Header().Get(httpx.RequestIDHeader))
		}
		// The other tenant's two keys must not appear on this span at all — the
		// smear on the span would be as wrong as the smear on the number, and the
		// recorder holds both tenants' spans by now.
		for _, kv := range span.Attributes() {
			if v := kv.Value.AsString(); v == twoTenants["acme"].String() && r.id != twoTenants["acme"] {
				t.Errorf("%s: span carries %s=%s, which is the other tenant's id", r.host, kv.Key, v)
			}
			if v := kv.Value.AsString(); v == "acme" && r.slug != "acme" {
				t.Errorf("%s: span carries %s=acme, which is the other tenant's slug", r.host, kv.Key)
			}
		}
	}
}

// --- helpers over the running totals a ManualReader keeps, so a case reads as deltas ---

// histCount is how many times the named operation was measured for one tenant. A
// histogram nothing has recorded into is absent from a collection altogether, so an
// absent instrument reads as zero rather than as a failure: the baseline is read
// before the case's own requests run, and the assertion is the delta.
func histCount(t *testing.T, operation string, tenant uuid.UUID) uint64 {
	t.Helper()
	m, ok := metrics(t)["pkit.http.operation.duration"]
	if !ok {
		return 0
	}
	h, ok := m.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("pkit.http.operation.duration is %T, want a histogram", m.Data)
	}
	var total uint64
	for _, p := range h.DataPoints {
		op, ok := p.Attributes.Value(attribute.Key("pkit.operation"))
		if !ok || op.AsString() != operation {
			continue
		}
		if id, ok := p.Attributes.Value(attribute.Key(telemetry.AttrTenantID)); ok && id.AsString() == tenant.String() {
			total += p.Count
		}
	}
	return total
}

// refusalSum is how many refusals of one class were counted for one named tenant.
// It sums the counter's own values, not the number of time series, so a class
// counted a hundred times against one attribute set reads as a hundred.
func refusalSum(t *testing.T, class string, tenant *uuid.UUID) int64 {
	t.Helper()
	var total int64
	for _, p := range refusalPoints(t) {
		v, found := p.Attributes.Value(attribute.Key(telemetry.AttrRefusalClass))
		if !found || v.AsString() != class {
			continue
		}
		id, hasID := p.Attributes.Value(attribute.Key(telemetry.AttrTenantID))
		if hasID && id.AsString() == tenant.String() {
			total += p.Value
		}
	}
	return total
}

// refusalClassTotal counts every refusal of one class, whoever it was attributed
// to — the total an operator reads first, before they ask whose.
func refusalClassTotal(t *testing.T, class string) int64 {
	t.Helper()
	var total int64
	for _, p := range refusalPoints(t) {
		v, found := p.Attributes.Value(attribute.Key(telemetry.AttrRefusalClass))
		if found && v.AsString() == class {
			total += p.Value
		}
	}
	return total
}

func refusalPoints(t *testing.T) []metricdata.DataPoint[int64] {
	t.Helper()
	m, ok := metrics(t)["pkit.http.refusals"]
	if !ok {
		return nil
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("pkit.http.refusals is %T, want a sum", m.Data)
	}
	return sum.DataPoints
}

// allPointAttrs is every attribute set every collected datapoint carries, of any
// instrument kind: a smear in the gauge would be as wrong as one in the counter.
func allPointAttrs(t *testing.T) []attribute.Set {
	t.Helper()
	var out []attribute.Set
	for _, m := range metrics(t) {
		switch d := m.Data.(type) {
		case metricdata.Histogram[float64]:
			for _, p := range d.DataPoints {
				out = append(out, p.Attributes)
			}
		case metricdata.Sum[int64]:
			for _, p := range d.DataPoints {
				out = append(out, p.Attributes)
			}
		case metricdata.Sum[float64]:
			for _, p := range d.DataPoints {
				out = append(out, p.Attributes)
			}
		case metricdata.Gauge[float64]:
			for _, p := range d.DataPoints {
				out = append(out, p.Attributes)
			}
		}
	}
	return out
}
