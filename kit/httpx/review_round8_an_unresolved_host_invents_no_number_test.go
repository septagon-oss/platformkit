package httpx_test

// Review round 8 (T-0110). This pins the metric form of a rule this delivery states
// twice in prose and its own case only reaches on the span:
//
//	kit/telemetry/README.md — "A key is written only where its value was learned —
//	a host the resolver does not know has neither."
//	kit/telemetry/telemetry.go — "a host the resolver does not know has neither key,
//	which is the honest answer."
//
// kit/httpx/tracing_test.go's TestAnUnresolvedHostCarriesNeitherTenantKey checks
// exactly those two keys on the *span*, then sends a second request and asserts only
// that refusals of class not_found were counted at all (>= 1). A counter answer
// satisfies that assertion whether it holds one series or one per sprayed host, and
// it never reads a datapoint's attributes — so the same rule, on the instrument where
// the key is part of a time series' identity, is asserted by nothing. Review round 3
// walked this path and named it as the thing it deliberately did not pin ("pins
// nothing that would resist a change either way"). That gap is closable in the
// direction the code already documents, because the alternative is not merely less
// informative:
//
//   - A tenant key is a metric attribute, so it is part of a time series' identity.
//     A key filled in from the Host header of a request whose host resolved nothing
//     turns every name an attacker sprays at port 443 into a new series of
//     pkit.http.refusals. One request, one series, one row in the collector's index:
//     that is a memory-exhaustion write to the monitoring path from an unauthenticated
//     socket, and it is also an attribution — a number filed under a customer who
//     does not exist.
//   - The same rule is the tenant boundary in metric form. Nothing else in the tree
//     fails if tenant.go starts stamping a guess; the refusal is still a 404, and the
//     counter still moves by one.
//
// So the case sprays N distinct hosts that the loader refuses, reaches its
// assertions through the status the router answered (404, from tenant.go's
// ErrNoSuchHost branch) and through the keys the counter carries — never through a
// sentence a refusal prints — and requires that N refusals were counted on exactly
// one series that names neither pkit.tenant nor pkit.tenant.id, and that no
// tenant's number moved. Every assertion here passes at this commit and fails the
// moment a tenant is invented for a host nobody resolved, in either direction: a
// stamped slug, or a per-host series.

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

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// round8Tenant is the one host this router does resolve, under an id no other case
// in this test binary can move: the assertion "nobody else's number moved" needs an
// id that is nobody else's, not a shared constant.
var round8Tenant = uuid.MustParse("8f8f8f8f-8f8f-4f8f-8f8f-8f8f8f8f8f8f")

const round8Permission = "round8:read"

// round8Router resolves `host` and refuses every other name with the loader's own
// ErrNoSuchHost — the answer a spray gets. The private operation exists so the
// request reaches the tenant middleware with something to protect: a public
// operation is served even when the host resolves nothing (tenant.go), which is a
// different promise, and this file would silently assert nothing if it asked the
// wrong one.
func round8Router(t *testing.T) http.Handler {
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
			return tenancy.Tenant{ID: round8Tenant, Slug: "acme8", Name: "Round Eight"}, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return true, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	httpx.Register(api.Surfaces("round8").App, huma.Operation{
		OperationID: "round8_private", Method: http.MethodGet, Path: "/private",
	}, httpx.Permission(round8Permission), func(context.Context, *struct{}) (*struct{}, error) {
		t.Errorf("the handler ran, so the request was served and the 404 below is not the answer")
		return &struct{}{}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the route does not declare itself: %v", err)
	}
	return router
}

// round8Attrs renders an attribute set for a failure message, keys and values in order.
func round8Attrs(set attribute.Set) string {
	var parts []string
	for _, kv := range set.ToSlice() {
		parts = append(parts, string(kv.Key)+"="+kv.Value.Emit())
	}
	return "{" + strings.Join(parts, " ") + "}"
}

// round8Series is the identity of a refusal datapoint as the collector sees it: its
// class and the full attribute set it was recorded under.
type round8Series struct {
	class string
	attrs attribute.Set
}

// round8Refusals snapshots every refusal series the reader holds right now. The
// reader is cumulative (kit/httpx/tracing_test.go says so), so the case compares
// against itself rather than trying to empty the reader.
func round8Refusals(t *testing.T) map[round8Series]int64 {
	t.Helper()
	out := map[round8Series]int64{}
	m, ok := metrics(t)["pkit.http.refusals"]
	if !ok {
		return out
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("pkit.http.refusals is %T, want a sum", m.Data)
	}
	for _, p := range sum.DataPoints {
		class := ""
		if v, found := p.Attributes.Value(attribute.Key(telemetry.AttrRefusalClass)); found {
			class = v.AsString()
		}
		out[round8Series{class: class, attrs: p.Attributes}] += p.Value
	}
	return out
}

// TestRefusalsOfHostsThatResolveNothingInventNeitherATenantNorASeries is the pin.
func TestRefusalsOfHostsThatResolveNothingInventNeitherATenantNorASeries(t *testing.T) {
	router := round8Router(t)
	const spray = 8

	before := round8Refusals(t)

	var hosts []string
	for i := range spray {
		h := fmt.Sprintf("spray-%d.test", i)
		hosts = append(hosts, h)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+h+"/api/v1/round8/private", nil))
		// The reachability probe is the status the fixed behaviour answers with —
		// tenant.go's ErrNoSuchHost branch, http.StatusNotFound — and not anything
		// a refusal says.
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET at the unresolvable %s was answered %d, want the 404 a host with no site gets: %s",
				h, rec.Code, rec.Body.String())
		}
	}

	after := round8Refusals(t)

	// 1. Nothing that already existed changed, and every new series is a refusal of
	//    a host nobody resolved: the delta is the spray, counted once per request.
	var (
		added       int64
		newSeries   int
		classed     = map[string]int64{}
		tenantedNew []string
	)
	for s, now := range after {
		grew := now - before[s]
		if grew < 0 {
			t.Fatalf("the refusal series {class %q, %s} went down from %d to %d", s.class, round8Attrs(s.attrs), before[s], now)
		}
		if grew == 0 {
			continue
		}
		newSeries++
		added += grew
		classed[s.class] += grew
		if _, hasID := s.attrs.Value(attribute.Key(telemetry.AttrTenantID)); hasID {
			tenantedNew = append(tenantedNew, round8Attrs(s.attrs))
		}
		if _, hasSlug := s.attrs.Value(attribute.Key(telemetry.AttrTenant)); hasSlug {
			tenantedNew = append(tenantedNew, round8Attrs(s.attrs))
		}
	}
	if added != spray {
		t.Errorf("the %d refusals moved the counter by %d, want %d: %v", spray, added, spray, classed)
	}
	if classed["not_found"] != spray {
		t.Errorf("the %d refusals of an unresolvable host were classed %v, want all of them %q",
			spray, classed, "not_found")
	}

	// 2. The tenant boundary in metric form: none of the new series names a tenant.
	if len(tenantedNew) > 0 {
		t.Errorf("%d refusals of hosts that resolved nothing (%v) were counted under a tenant key: %v",
			added, hosts, tenantedNew)
	}

	// 3. The cardinality promise: N distinct attacker-chosen hosts bought exactly one
	//    new series, not one per host. This is what a collector's index is priced in.
	if newSeries != 1 {
		t.Errorf("%d refusals across %d distinct hosts created %d new time series, want 1 — a key read from "+
			"the Host header makes one series per name a client can type", added, spray, newSeries)
	}

	// 4. And the tenant that does exist was not the one these requests were filed against.
	for s, now := range after {
		if _, hasID := s.attrs.Value(attribute.Key(telemetry.AttrTenantID)); !hasID {
			continue
		}
		if id, _ := s.attrs.Value(attribute.Key(telemetry.AttrTenantID)); id.AsString() == round8Tenant.String() {
			if grew := now - before[s]; grew != 0 {
				t.Errorf("the tenant that resolved (%s) was charged %d of the refusals at hosts that resolved nothing",
					round8Tenant, grew)
			}
		}
	}
}
