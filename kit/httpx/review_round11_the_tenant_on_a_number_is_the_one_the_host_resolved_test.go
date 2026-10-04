package httpx_test

// Review round 11 (T-0110). Two assertions about the tenant dimension that no case
// in this tree holds, both reached through what the router answers (a status, a
// response header) and through the keys a span or a datapoint carries — never
// through a sentence a refusal prints.
//
//  1. The tenant on a number is decided by the resolver and by nothing else. Every
//     existing case feeds the router a *host* and asks which tenant the span and the
//     number name. Nothing feeds the same router a `baggage:` header naming somebody
//     else's tenant, which is the other channel a caller holds: kit/telemetry carries
//     the request id as W3C Baggage, kit/httpx installs propagation.Baggage{} in the
//     set the router extracts on, and telemetry.SpanAttrs reads a context. If the
//     tenant were ever read out of the carried bag rather than out of
//     tenancy.FromContext, an unauthenticated caller could file its own refusals,
//     latency and spans under another tenant's key — the exact thing this delivery
//     exists to refuse, and it would stay green in every existing case, because the
//     HTTP answer would still be correct.
//  2. The note that carries the tenant to the refusal counter lives as long as the
//     request that filled it. kit/httpx/respond.go allocates it per request today and
//     traced.go says why ("Nothing guards it: the write happens inside the call stack
//     of the read"); the natural next edit is to hoist it onto the router, which is
//     correct for everything except the count. Round 3 pins that each of two tenants
//     is charged its own, and round 8 pins that a host resolving nothing invents no
//     tenant — but each of those asks a router that never served the *other* case
//     first. A hoisted note charges the second request's 404 to the first request's
//     tenant, and both existing cases stay green.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// round11ID is this router's tenant, under an id no other case in this binary moves.
var round11ID = uuid.MustParse("11e11e11-11e1-41e1-81e1-11e11e11e111")

// round11Foreign names somebody else: the tenant this case puts in a header and the
// kernel must never write anywhere. The strings are unique to this file, so a scan of
// every span and datapoint in the process is a scan for exactly these.
var (
	round11ForeignID   = uuid.MustParse("f07e11f0-11f0-41f0-81f0-f07e11f07e11")
	round11ForeignSlug = "globex-round11"
)

const (
	round11Permission = "round11:read"
	round11ForgedID   = "the-request-id-the-caller-invented"
)

// round11Router resolves one host, denies round11Permission, and refuses every other
// name with the loader's own ErrNoSuchHost.
func round11Router(t *testing.T) http.Handler {
	t.Helper()
	_, app := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host,
		Conn:       app,
		Cache:      cache.Memory("pkit"),
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
			if h != host {
				return tenancy.Tenant{}, tenancy.ErrNoSuchHost
			}
			return tenancy.Tenant{ID: round11ID, Slug: "acme11", Name: "Round Eleven"}, nil
		}),
		Authorize: authorizerFunc(func(_ context.Context, _ tenancy.Tenant, g tenancy.Grant) (bool, error) {
			return g.Permission != round11Permission, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	httpx.Register(api.Surfaces("round11").App, huma.Operation{
		OperationID: "round11_ping", Method: http.MethodGet, Path: "/ping",
	}, httpx.Public(), func(context.Context, *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	})
	httpx.Register(api.Surfaces("round11").App, huma.Operation{
		OperationID: "round11_secret", Method: http.MethodGet, Path: "/secret",
	}, httpx.Permission(round11Permission), func(context.Context, *struct{}) (*struct{}, error) {
		t.Errorf("the guard let a caller through to round11_secret, so the 403 below is not a refusal")
		return &struct{}{}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the routes do not declare themselves: %v", err)
	}
	return router
}

// round11Sent is one request through the router, answered as the client saw it.
func round11Sent(t *testing.T, router http.Handler, authority, path string, header [][2]string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://"+authority+path, nil)
	for _, h := range header {
		req.Header.Set(h[0], h[1])
	}
	router.ServeHTTP(rec, req)
	return rec
}

// round11Poisoned reports an attribute whose value names the tenant this caller
// invented. Both keys are checked, and by value rather than by key, because the
// defect this case refuses does not have to use a pkit. key to be the defect:
// writing the forged slug under http.host would be the same lie.
func round11Poisoned(kv attribute.KeyValue) bool {
	v := kv.Value.Emit()
	return strings.Contains(v, round11ForeignID.String()) || strings.Contains(v, round11ForeignSlug)
}

// round11Points is every attribute set every instrument holds right now, with the
// instrument named beside it. The reader is cumulative, so the case looks for the
// caller's own strings rather than trying to empty it.
func round11Points(t *testing.T) map[string][]attribute.Set {
	t.Helper()
	out := map[string][]attribute.Set{}
	for name, m := range metrics(t) {
		var sets []attribute.Set
		switch d := m.Data.(type) {
		case metricdata.Histogram[float64]:
			for _, p := range d.DataPoints {
				sets = append(sets, p.Attributes)
			}
		case metricdata.Gauge[float64]:
			for _, p := range d.DataPoints {
				sets = append(sets, p.Attributes)
			}
		case metricdata.Sum[int64]:
			for _, p := range d.DataPoints {
				sets = append(sets, p.Attributes)
			}
		case metricdata.Sum[float64]:
			for _, p := range d.DataPoints {
				sets = append(sets, p.Attributes)
			}
		}
		out[name] = sets
	}
	return out
}

// TestACallerNamesNoTenantOnASpanOrANumberItDidNotResolve is assertion 1: the header
// channel carries a tenant, the resolver answers a different one, and every number
// and span this process emits names the resolver's.
func TestACallerNamesNoTenantOnASpanOrANumberItDidNotResolve(t *testing.T) {
	router := round11Router(t)
	spans.Reset()

	rec := round11Sent(t, router, host, "/api/v1/round11/ping", [][2]string{
		{"Baggage", "pkit.tenant=" + round11ForeignSlug +
			",pkit.tenant.id=" + round11ForeignID.String() +
			",pkit.request.id=" + round11ForgedID},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("the public route answered %d, want it to reach the handler: %s", rec.Code, rec.Body.String())
	}

	// The span the router opened carries the tenant the host resolved.
	var server []string
	for _, s := range spans.Ended() {
		if s.SpanKind() != trace.SpanKindServer {
			continue
		}
		server = append(server, s.Name())
		for key, want := range map[string]string{telemetry.AttrTenant: "acme11", telemetry.AttrTenantID: round11ID.String()} {
			got, found := attributeOf(s, key)
			if !found || got.Emit() != want {
				t.Errorf("%s %s = %v (found %v), want %q", s.Name(), key, got, found, want)
			}
		}
	}
	if len(server) != 1 {
		t.Fatalf("%d server spans (%v), want the one this request is", len(server), server)
	}

	// And nothing, on either surface, carries the tenant the header named.
	round11NothingNames(t, "a tenant named in the caller's baggage and resolved by nobody")

	// The same header at a host the loader refuses, with an id Baggage itself will not
	// carry (kit/httpx accepts a comma; W3C refuses one) so the caller's bag is the bag
	// the request keeps. This is where a bag-read tenant lands: with nothing resolved
	// there is no tenancy value to prefer, and a kernel that filled the gap from the
	// carried bag would file an anonymous request's refusals under a customer named in
	// a header. Round 8 pins the Host-header form of this rule; nothing pinned its
	// baggage form.
	spans.Reset()
	rec = round11Sent(t, router, "nobody-round11.test", "/api/v1/round11/secret", [][2]string{
		{"X-Request-ID", "a,b,c"},
		{"Baggage", "pkit.tenant=" + round11ForeignSlug + ",pkit.tenant.id=" + round11ForeignID.String()},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("the unresolvable host answered %d, want the 404 it gets: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get(httpx.RequestIDHeader) != "a,b,c" {
		t.Fatalf("the kernel answered %q as the request id, want the caller's own id kept in the header", rec.Header().Get(httpx.RequestIDHeader))
	}
	round11NothingNames(t, "a tenant named in a header, at a host that resolved no tenant")
}

// round11NothingNames scans every span this process recorded and every attribute set
// every instrument holds, and refuses any value that names the tenant the caller
// invented. By value rather than by key: writing the forged slug under http.host would
// be the same lie with a tidier key.
func round11NothingNames(t *testing.T, why string) {
	t.Helper()
	for _, s := range spans.Ended() {
		for _, kv := range s.Attributes() {
			if round11Poisoned(kv) {
				t.Errorf("span %q carries %s=%s — %s", s.Name(), kv.Key, kv.Value.Emit(), why)
			}
		}
	}
	for name, sets := range round11Points(t) {
		for _, set := range sets {
			for _, kv := range set.ToSlice() {
				if round11Poisoned(kv) {
					t.Errorf("%s carries %s=%s — %s", name, kv.Key, kv.Value.Emit(), why)
				}
			}
		}
	}
}

// TestEverySpanOfARequestNamesTheRequestTheCallerWasAnsweredFor is assertion 2: the
// join an operator actually performs is quoting the X-Request-ID in a response, a log
// line and a ticket, and kit/telemetry's package comment promises the id "is *stamped*
// as well, on the spans this kernel opens itself". With an id Baggage itself will not
// carry — kit/httpx accepts any printable ASCII to 64 bytes, and W3C refuses "=" — the
// kernel leaves the bag alone, and the bag the caller sent arrives with it.
func TestEverySpanOfARequestNamesTheRequestTheCallerWasAnsweredFor(t *testing.T) {
	router := round11Router(t)
	spans.Reset()

	const given = "round11=id=baggage=refuses=this"
	rec := round11Sent(t, router, host, "/api/v1/round11/ping", [][2]string{
		{"X-Request-ID", given},
		{"Baggage", "pkit.request.id=" + round11ForgedID},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("the public route answered %d, want it to reach the handler: %s", rec.Code, rec.Body.String())
	}
	answered := rec.Header().Get(httpx.RequestIDHeader)
	if answered != given {
		t.Fatalf("%s = %q, want the id the caller sent, which kit/httpx's givenID accepts: %q",
			httpx.RequestIDHeader, answered, given)
	}

	var seen int
	for _, s := range spans.Ended() {
		id, found := attributeOf(s, telemetry.AttrRequestID)
		if !found {
			continue
		}
		seen++
		if id.Emit() != answered {
			t.Errorf("%s names %s=%s as the request that caused it, while the caller was answered %s: an "+
				"operator quoting the id in the response finds no span, and the span it does find names a request "+
				"nobody was answered for", s.Name(), telemetry.AttrRequestID, id.Emit(), answered)
		}
	}
	if seen == 0 {
		t.Fatal("no span of this request names a request at all, so the assertion above had nothing to read")
	}
}

// TestARefusalTheResolverCouldNotAttributeNamesNoTenantAfterATenantWasServed is
// assertion 3, the lifetime of the note: a request the loader refused is counted on a
// series naming no tenant, even though the same router counted a refusal for a tenant
// one request earlier.
func TestARefusalTheResolverCouldNotAttributeNamesNoTenantAfterATenantWasServed(t *testing.T) {
	router := round11Router(t)

	forbidden := func() int64 { return refusalSum(t, "forbidden", &round11ID) }
	beforeDenied := forbidden()

	rec := round11Sent(t, router, host, "/api/v1/round11/secret", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("the guarded route answered %d, want the 403 the denial gets: %s", rec.Code, rec.Body.String())
	}
	if grew := forbidden() - beforeDenied; grew != 1 {
		t.Fatalf("the refusal of acme11's own request moved acme11's forbidden count by %d, want 1", grew)
	}

	beforeSeries := round11Refusals(t)
	beforeAll := refusalClassTotal(t, "not_found")
	rec = round11Sent(t, router, "unattributed-round11.test", "/api/v1/round11/secret", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a host the loader does not know answered %d, want the 404 that answers it: %s",
			rec.Code, rec.Body.String())
	}
	if got := refusalClassTotal(t, "not_found") - beforeAll; got != 1 {
		t.Fatalf("the 404 moved class not_found by %d, want 1", got)
	}

	// The tenant that was served one request earlier was not charged for this one.
	if grew := forbidden() - beforeDenied - 1; grew != 0 {
		t.Errorf("acme11 was charged %d further refusals after its own request, from a host that resolved nothing", grew)
	}

	// And the series this 404 was counted on names no tenant. The reader is cumulative
	// across the whole binary — every other case in this package has recorded its own
	// refusals under its own tenant by now — so the assertion is a delta: the series
	// this request added is the one under examination, and it bought exactly one.
	var (
		added    int64
		tenanted []string
	)
	for series, now := range round11Refusals(t) {
		grew := now - beforeSeries[series]
		if grew < 0 {
			t.Fatalf("the series %s went down from %d to %d between two requests", series, beforeSeries[series], now)
		}
		if grew == 0 {
			continue
		}
		added += grew
		if strings.Contains(series, telemetry.AttrTenant+"=") || strings.Contains(series, telemetry.AttrTenantID+"=") {
			tenanted = append(tenanted, fmt.Sprintf("%s (+%d)", series, grew))
		}
	}
	if added != 1 {
		t.Errorf("the 404 at an unresolvable host moved the refusal counter by %d, want 1", added)
	}
	if len(tenanted) > 0 {
		t.Errorf("a refusal at a host that resolved nothing was counted on %d series naming a tenant this "+
			"request never resolved: %v", len(tenanted), tenanted)
	}
}

// round11Refusals is every refusal series the reader holds, rendered as
// "<class> {key=value ...}" -> count, so a series' whole identity is comparable and
// printable in the same breath.
func round11Refusals(t *testing.T) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	m, ok := metrics(t)["pkit.http.refusals"]
	if !ok {
		return out
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("pkit.http.refusals is %T, want a sum", m.Data)
	}
	for _, p := range sum.DataPoints {
		var parts []string
		for _, kv := range p.Attributes.ToSlice() {
			parts = append(parts, string(kv.Key)+"="+kv.Value.Emit())
		}
		class := ""
		if v, found := p.Attributes.Value(attribute.Key(telemetry.AttrRefusalClass)); found {
			class = v.AsString()
		}
		out[class+" {"+strings.Join(parts, " ")+"}"] += p.Value
	}
	return out
}
