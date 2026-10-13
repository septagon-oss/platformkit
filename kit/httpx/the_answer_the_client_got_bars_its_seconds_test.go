package httpx_test

// This file pins
// one sentence of the kernel's own promise, as a measurement rather than a
// reading: what every answer the router gives does to the latency histogram.
//
//	kit/telemetry/instruments.go: "Refusals counts the answers the kernel gave a
//	client it would not serve ... It counts refusals and not errors: a 500 the
//	handler returned is already in the latency histogram with an error status on
//	its span"
//
// and `kit/httpx/traced.go`'s own promise, "one latency histogram per operation".
// The claim is about *where a request lands* — the histogram is the denominator
// every latency question divides by — so the case is: for each way this router can
// answer, the request that reached a declared operation moved
// `pkit.http.operation.duration` by exactly one bar, carrying the tenant the host
// resolved and that operation's id. A bar missing for one answer shape is invisible
// to a counter that only asks whether the good path records one: an operator reads
// "refusals up, latency flat" and triages the wrong thing.
//
// Reachability is the status the router answered with, printed by the case itself,
// never the histogram's absence; a request that reached no operation at all is
// asserted to move nothing, which is the other half of the promise.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// r22Tenant is the tenant this fixture's host resolves to, named once so the
// fixture and the assertion cannot drift into two tenants.
var r22Tenant = uuid.MustParse("33333333-3333-4333-8333-333333333333")

// r22Fixture serves one operation per way this router answers: a held 200, a
// handler that returns an error, a handler that panics, a guard refusal, an
// unmounted address and an unresolved host.
func r22Fixture(t *testing.T) http.Handler {
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
			return tenancy.Tenant{ID: r22Tenant, Slug: "acme", Name: "Acme"}, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return true, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "r22_ok", Method: http.MethodGet, Path: "/ok",
	}, httpx.Public(), func(context.Context, *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	})
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "r22_error", Method: http.MethodGet, Path: "/error",
	}, httpx.Public(), func(context.Context, *struct{}) (*struct{}, error) {
		return nil, context.DeadlineExceeded
	})
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "r22_panic", Method: http.MethodGet, Path: "/panic",
	}, httpx.Public(), func(context.Context, *struct{}) (*struct{}, error) {
		panic("the handler fell over")
	})
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "r22_signed_in", Method: http.MethodGet, Path: "/signed-in",
	}, httpx.SignedIn(), func(context.Context, *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the routes do not declare themselves: %v", err)
	}
	return router
}

// TestEveryAnswerOfADeclaredOperationBarsExactlyOneLatency runs the promise per
// answer: one request that reached an operation moves that operation's histogram by
// exactly one bar, named by the tenant the host resolved.
func TestEveryAnswerOfADeclaredOperationBarsExactlyOneLatency(t *testing.T) {
	router := r22Fixture(t)
	const base = "http://" + host + "/api/v1/probe"

	cases := []struct {
		what string
		op   string
		req  func() *http.Request
	}{
		{"the handler answered", "r22_ok", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, base+"/ok", nil)
		}},
		{"the handler returned an error", "r22_error", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, base+"/error", nil)
		}},
		{"the handler panicked and the recovery answered 500", "r22_panic", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, base+"/panic", nil)
		}},
		{"a guard refused an anonymous caller at a signed-in route", "r22_signed_in", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, base+"/signed-in", nil)
		}},
	}

	for _, tc := range cases {
		before := r22Bars(t)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, tc.req())
		if rec.Code == 0 {
			t.Fatalf("%s: the router answered nothing, so this case asked the histogram no question", tc.what)
		}
		after := r22Bars(t)
		key := tc.op + "|" + r22Tenant.String()
		if got := after[key] - before[key]; got != 1 {
			t.Errorf("%s: the client was answered %d and pkit.http.operation.duration moved by %d "+
				"bars for operation %q and tenant %s, want exactly 1 — the histogram is the "+
				"denominator of every latency question, and an answer shape that records no bar "+
				"makes the instrument read a request that never happened as one that was fast",
				tc.what, rec.Code, got, tc.op, r22Tenant)
		}
	}
}

// TestAnAnswerOfNoOperationBarsNothingIs the other half of the same sentence: the
// delivery says the histogram holds a *registered operation's* seconds, so a request
// that reached no operation must not move one — and a bar carrying a tenant the
// request never resolved is the number this runtime promises not to publish.
func TestAnAnswerOfNoOperationBarsNothing(t *testing.T) {
	router := r22Fixture(t)

	cases := []struct {
		what string
		req  func() *http.Request
	}{
		{"the host names no site, at a route that is not public", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://nowhere.test/api/v1/probe/signed-in", nil)
		}},
		{"nobody mounted that address", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://"+host+"/api/v1/probe/nowhere", nil)
		}},
		{"that module is not composed", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://"+host+"/api/v1/nomodule/anything", nil)
		}},
	}
	for _, tc := range cases {
		before := r22Bars(t)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, tc.req())
		if rec.Code < http.StatusBadRequest {
			t.Errorf("%s: the client was answered %d, which is not the refusal this case is about",
				tc.what, rec.Code)
			continue
		}
		after := r22Bars(t)
		for key, delta := range r22Diff(before, after) {
			if delta != 0 {
				t.Errorf("%s: the client was answered %d at a request that reached no operation, "+
					"and pkit.http.operation.duration moved by %d for %s", tc.what, rec.Code, delta, key)
			}
		}
	}
}

// r22Bars is every latency bar the process has recorded, keyed by
// "<operation>|<tenant id>", read off the ManualReader the binary installs. A
// series nobody recorded is absent from a collection rather than present at zero,
// so absence is this number's zero.
func r22Bars(t *testing.T) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	m, ok := metrics(t)["pkit.http.operation.duration"]
	if !ok {
		return out
	}
	hist, ok := m.Data.(sdkmetric.Histogram[float64])
	if !ok {
		t.Fatalf("pkit.http.operation.duration is %T, want a histogram", m.Data)
	}
	for _, p := range hist.DataPoints {
		op, hasOp := p.Attributes.Value(attribute.Key("pkit.operation"))
		tid, hasTID := p.Attributes.Value(attribute.Key(telemetry.AttrTenantID))
		if !hasOp || !hasTID {
			t.Errorf("a pkit.http.operation.duration datapoint carries pkit.operation=%v pkit.tenant.id=%v; "+
				"a latency bar that names neither the operation nor the tenant cannot be asked of anybody",
				hasOp, hasTID)
			continue
		}
		out[op.AsString()+"|"+tid.AsString()] += int64(p.Count)
	}
	return out
}

func r22Diff(before, after map[string]int64) map[string]int64 {
	out := map[string]int64{}
	for k, v := range after {
		out[k] = v - before[k]
	}
	for k, v := range before {
		if _, ok := after[k]; !ok {
			out[k] = -v
		}
	}
	return out
}
