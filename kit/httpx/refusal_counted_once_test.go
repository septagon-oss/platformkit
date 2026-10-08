package httpx_test

// Whether every refusal reaches the counter exactly once is a two-part claim, and
// the cure's own words make it that way:
//
//	fix(httpx): every refusal is counted, once, from the answer the client got
//	... respond counts the status the client was finally given, once ...
//	It also fixes the double count a per-writer counter invites, because the
//	buffer replaces an answer (a held 200 becomes a 500 when the commit fails, a
//	panic replaces whatever was written) ...                  (1deb242, traced.go)
//
// The three cases in refusal_counter_test.go ask one question — "did the number
// notice at all" (got < 1)
// — and the cure answers it. Nobody on either side asks the second half, which is
// the half a fix that *relocates* a count can break while keeping the first true:
// that one refused request moves the counter by exactly one. A count left behind at
// a writer below the buffer, a refusal refused at a gate and again by the router, or
// the same answer written once with a tenant and once without all read as "counted"
// to a case that accepts any movement, and as a number an operator cannot sum to a
// request count.
//
// Each case is one request; it asserts the class its own answer belongs to moved by
// one, and that no other class moved at all. Every case reaches its assertion
// through what the router prints — the status it answered with, and the class the
// delivery's own table gives that status — and never through the counter's failure
// mode: a request answered 200 fails its own status assertion first, so no case can
// pass by refusing nothing.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// onceFixture is the router these cases run against: a public route that reads a
// query value, a signed-in route, a handler that returns an error, and a handler
// that panics. Same host and tenant as refusal_counter_test.go's fixture, built here
// so that file keeps the bytes its author wrote.
func onceFixture(t *testing.T) http.Handler {
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
			return tenancy.Tenant{ID: reviewTenant, Slug: "acme", Name: "Acme"}, nil
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
		OperationID: "once_public_query", Method: http.MethodGet, Path: "/public-query",
	}, httpx.Public(), func(context.Context, *reviewInput) (*struct{}, error) {
		return &struct{}{}, nil
	})
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "once_signed_in", Method: http.MethodGet, Path: "/query",
	}, httpx.SignedIn(), func(context.Context, *reviewInput) (*struct{}, error) {
		return &struct{}{}, nil
	})
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "once_failing", Method: http.MethodGet, Path: "/failing",
	}, httpx.Public(), func(context.Context, *struct{}) (*struct{}, error) {
		return nil, context.DeadlineExceeded
	})
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "once_panicking", Method: http.MethodGet, Path: "/panicking",
	}, httpx.Public(), func(context.Context, *struct{}) (*struct{}, error) {
		panic("the handler fell over")
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the routes do not declare themselves: %v", err)
	}
	return router
}

// TestOneRefusedRequestMovesItsClassByExactlyOne is the commit subject, run: one
// request, one count, whatever shape the refusal took.
func TestOneRefusedRequestMovesItsClassByExactlyOne(t *testing.T) {
	router := onceFixture(t)
	const base = "http://" + host + "/api/v1/probe"

	cases := []struct {
		what string
		req  func() *http.Request
	}{
		{"the router refused a query value that is not an integer", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, base+"/public-query?limit=many", nil)
		}},
		{"a guard refused an anonymous caller at a signed-in route", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, base+"/query?limit=5", nil)
		}},
		{"a handler returned an error", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, base+"/failing", nil)
		}},
		{"a handler panicked and the recovery replaced the answer", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, base+"/panicking", nil)
		}},
		{"nobody mounted that address", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, base+"/nobody-mounted-this", nil)
		}},
		{"nobody mounted that module", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://"+host+"/api/v1/nomodule/anything", nil)
		}},
		{"the method is not one the route takes", func() *http.Request {
			return httptest.NewRequest(http.MethodDelete, base+"/public-query", nil)
		}},
		{"the body is not one the route takes", func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, base+"/public-query", nil)
			r.Header.Set("Content-Type", "text/plain")
			return r
		}},
	}

	for _, tc := range cases {
		before := classTotals(t)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, tc.req())

		if rec.Code < http.StatusBadRequest {
			// The reachability probe, off the status alone: a request that was never
			// refused says so here, rather than the case passing at a motionless
			// counter.
			t.Errorf("%s: the client was answered %d, which is not a refusal, so this case "+
				"asked the counter nothing: %s", tc.what, rec.Code, rec.Body.String())
			continue
		}
		class := telemetry.RefusalClass(rec.Code)
		if class == "unknown" {
			t.Errorf("%s: the client was answered %d, which the closed class set maps to %q, so "+
				"the number files this refusal beside every other answer nobody mapped",
				tc.what, rec.Code, class)
			continue
		}
		after := classTotals(t)
		if got := after[class] - before[class]; got != 1 {
			t.Errorf("%s: the client was answered %d (class %q) and pkit.http.refusals moved by "+
				"%d for that one request, want exactly 1 — the cure promises every refusal is "+
				"counted once, and a class that counts one refusal twice cannot be summed to a "+
				"number of refused requests", tc.what, rec.Code, class, got)
		}
		delete(after, class)
		for other, delta := range after {
			if delta != before[other] {
				t.Errorf("%s: the client was answered %d (class %q), and class %q moved by %d as "+
					"well — one refusal recorded under two classes is counted twice, under a name "+
					"an operator would not think to add",
					tc.what, rec.Code, class, other, delta-before[other])
			}
		}
	}
}

// classTotals is the running total per class, read the way
// refusal_counter_test.go reads it. An instrument nothing has recorded on is absent
// from a collection rather than present at zero, so absence is this number's zero.
func classTotals(t *testing.T) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	m, ok := metrics(t)["pkit.http.refusals"]
	if !ok {
		return out
	}
	sum, ok := m.Data.(sdkmetric.Sum[int64])
	if !ok {
		t.Fatalf("pkit.http.refusals is %T, want a sum", m.Data)
	}
	for _, p := range sum.DataPoints {
		v, found := p.Attributes.Value(attribute.Key(telemetry.AttrRefusalClass))
		if !found {
			t.Errorf("a pkit.http.refusals datapoint carries no %s, so the number cannot be read by class",
				telemetry.AttrRefusalClass)
			continue
		}
		out[v.AsString()] += p.Value
	}
	return out
}
