package httpx_test

// A route's declaration is the only unbounded-looking thing a measurement may
// quote, because it is the only one the operator wrote.
//
// `pkit.operation`, `http.route` and the span name are decided in `traced.go`, which
// reads `ctx.Operation()` — the operation the router matched, whose `Path` is the
// pattern (`/api/v1/reading/thing/{id}`), not the request's target
// (`/api/v1/reading/thing/0f4a…`). The difference is invisible on a route without a
// path parameter, and it is the whole of what a path parameter means for
// measurement: a span attribute or a datapoint attribute assembled from the target
// is a value a caller types. On a span that is a trace backend whose index grows
// with traffic; on a datapoint it is worse, because attribute values are part of a
// time series' identity, so a caller asking for a thousand ids is owed a thousand
// series for one operation, and the operation's latency stops being averageable —
// the failure mode `TestANumberCarriesNothingThatNamesOneRequest` refuses for the
// request id, which is the value that happens to be unique already.
//
// Nothing here needs a defect to be visible, and nothing here asks for one: the
// cases send three ids through one route, ask that each was served (a status and a
// request id in the response prove the request got through), and then ask what the
// span and the datapoint say about which operation it was. `traced.go` is the only
// place that answers, and it can answer wrongly in one edit — swapping `op.Path` for
// the target is the natural mistake for anyone reading the middleware before chi
// has resolved a route, which is exactly the moment the middleware runs.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/attribute"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const (
	thingOperation = "reading_thing"
	thingPattern   = "/thing/{id}"
	thingMounted   = "/api/v1/reading/thing/{id}"
)

// thingValues are what one caller asks for. They are unique to this file, so a scan
// of every attribute this process recorded is a scan for exactly these.
var thingValues = []string{"aaaa-1111", "bbbb-2222", "cccc-3333"}

// thingRouter serves one operation behind one path parameter, for the tenant
// tracedFixture already resolves.
func thingRouter(t *testing.T) http.Handler {
	t.Helper()
	_, app := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host,
		Conn:       app,
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
			if h != host {
				return tenancy.Tenant{}, tenancy.ErrNoSuchHost
			}
			return tenancy.Tenant{ID: tenantID, Slug: "acme", Name: "Acme"}, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return true, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	httpx.Register(api.Surfaces("reading").App, huma.Operation{
		OperationID: thingOperation, Method: http.MethodGet, Path: thingPattern,
	}, httpx.Public(), func(context.Context, *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the route does not declare itself: %v", err)
	}
	return router
}

// serveThing sends one request for one id and refuses to let the case below prove
// nothing: the reachability proof is the answer the client was given, and it is a
// 204 with a request id in the response, which is what the fixed behaviour prints.
func serveThing(t *testing.T, router http.Handler, value string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+host+"/api/v1/reading/thing/"+value, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("GET /api/v1/reading/thing/%s answered %d, want the 204 the route gives: %s",
			value, rec.Code, rec.Body.String())
	}
	if rec.Header().Get(httpx.RequestIDHeader) == "" {
		t.Fatalf("GET /api/v1/reading/thing/%s carries no %s, so no measurement below names this request",
			value, httpx.RequestIDHeader)
	}
	return rec
}

// TestASpanNamesTheDeclaredPatternAndNotThePathACallerAskedFor is the span's half:
// one operation, three targets, and the same name and route on every one of them.
func TestASpanNamesTheDeclaredPatternAndNotThePathACallerAskedFor(t *testing.T) {
	router := thingRouter(t)
	for _, value := range thingValues {
		spans.Reset()
		serveThing(t, router, value)
		span := serverSpan(t)
		if span.Name() != thingOperation {
			t.Errorf("the span for /api/v1/reading/thing/%s is named %q, want the operation id the declaration gives it",
				value, span.Name())
		}
		route, found := attributeOf(span, "http.route")
		if !found {
			t.Fatalf("/api/v1/reading/thing/%s left its span with no http.route attribute", value)
		}
		if route.Emit() != thingMounted {
			t.Errorf("http.route = %s, want the declared pattern %q: a value taken from the request target is a value a caller types, and one span attribute per id is a trace index that grows with traffic",
				route.Emit(), thingMounted)
		}
		// The keys this package writes are the ones this case can hold it to. The
		// server conventions' own attributes belong to otelhttp, and this repository
		// does not decide them.
		for _, kv := range span.Attributes() {
			key := string(kv.Key)
			if !strings.HasPrefix(key, "pkit.") && key != "http.route" {
				continue
			}
			if strings.Contains(kv.Value.Emit(), value) {
				t.Errorf("%s = %s on the span for /api/v1/reading/thing/%s: a value the caller typed stands in an attribute the operator declared",
					key, kv.Value.Emit(), value)
			}
		}
	}
}

// TestDistinctPathValuesAddNoTimeSeriesToTheirOperation is the number's half, and
// the one that costs a dashboard something: one operation served behind three
// different targets is one time series, not three.
func TestDistinctPathValuesAddNoTimeSeriesToTheirOperation(t *testing.T) {
	router := thingRouter(t)
	for _, value := range thingValues {
		serveThing(t, router, value)
	}
	var series []attribute.Set
	for _, p := range histogramPoints(t, "pkit.http.operation.duration") {
		op, ok := p.Attributes.Value(attribute.Key("pkit.operation"))
		if !ok || op.AsString() != thingOperation {
			continue
		}
		series = append(series, p.Attributes)
	}
	if len(series) != 1 {
		for _, s := range series {
			t.Logf("operation %s datapoint: %v", thingOperation, s.ToSlice())
		}
		t.Fatalf("operation %s holds %d datapoints after three targets, want the one series its declaration names: a datapoint attribute whose value comes from the request multiplies the series by what the caller asked for, and the operation's latency stops being averageable",
			thingOperation, len(series))
	}
	for _, kv := range series[0].ToSlice() {
		for _, value := range thingValues {
			if strings.Contains(kv.Value.Emit(), value) {
				t.Errorf("%s = %s on the operation's datapoint, a value assembled from a request target that named %s", kv.Key, kv.Value.Emit(), value)
			}
		}
	}
	for _, key := range []string{telemetry.AttrTenant, telemetry.AttrTenantID} {
		if _, has := series[0].Value(attribute.Key(key)); !has {
			t.Errorf("the operation's one datapoint carries no %s: %v", key, series[0].ToSlice())
		}
	}
}
