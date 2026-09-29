package httpx_test

// The trace a request leaves behind, read off the SDK's own in-memory recorder.
//
// The brief's done-criterion is "a request shows up as a trace with a tenant
// attribute", and the only honest way to check that in a repository with no
// collector in its test fixtures is to install a recorder as the process's provider
// and read the spans back. That is what TestMain does, once per binary: a tracer
// resolved before the first install stays bound to that first install, so this
// binary installs one recorder and one reader and every case reads what it needs
// out of them.
//
// Nothing here needs a collector, and nothing here proves anything about one: what
// is asserted is what this package puts on a span and on a number, which is the
// whole claim the kernel makes.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

var (
	spans    = tracetest.NewSpanRecorder()
	readings = sdkmetric.NewManualReader()
)

func TestMain(m *testing.M) {
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(readings)))
	otel.SetTextMapPropagator(telemetry.Propagators())
	os.Exit(m.Run())
}

// tenantID is the tenant this fixture resolves host to, named once so the
// assertion and the fixture cannot drift apart into two ids.
var tenantID = uuid.MustParse("33333333-3333-4333-8333-333333333333")

// tracedFixture is a router with one declared operation, resolving "acme.test" to
// one tenant and refusing every other host — which is the case worth having beside
// the good one, because a request that resolved no tenant must carry no tenant.
func tracedFixture(t *testing.T) http.Handler {
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
	httpx.Register(api.Surfaces("probe").App, huma.Operation{
		OperationID: "probe_ping", Method: http.MethodGet, Path: "/ping",
	}, httpx.Public(), func(context.Context, *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the route does not declare itself: %v", err)
	}
	return router
}

// TestARequestLeavesOneSpanCarryingItsTenantAndRequest is the brief's
// done-criterion: the operation names the span, the tenant is on it under both
// keys, and the request id the caller got back in X-Request-ID is on it too.
func TestARequestLeavesOneSpanCarryingItsTenantAndRequest(t *testing.T) {
	router := tracedFixture(t)
	spans.Reset()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+host+"/api/v1/probe/ping?token=secret", nil))
	if rec.Code >= 400 {
		t.Fatalf("the route answered %d, want it to reach the handler: %s", rec.Code, rec.Body.String())
	}
	span := serverSpan(t)
	if span.Name() != "probe_ping" {
		t.Errorf("span is named %q, want the operation id the OpenAPI document gives the call", span.Name())
	}
	for key, want := range map[string]string{
		telemetry.AttrTenant:   "acme",
		telemetry.AttrTenantID: tenantID.String(),
		"http.route":           "/api/v1/probe/ping",
	} {
		got, found := attributeOf(span, key)
		if !found {
			t.Errorf("%s has no %s attribute", span.Name(), key)
			continue
		}
		if got.Emit() != want {
			t.Errorf("%s %s = %s, want %q", span.Name(), key, got.Emit(), want)
		}
	}
	id, found := attributeOf(span, telemetry.AttrRequestID)
	if !found || id.Emit() != rec.Header().Get(httpx.RequestIDHeader) {
		t.Errorf("%s = %v (found %v), want the id in the response header %q",
			telemetry.AttrRequestID, id, found, rec.Header().Get(httpx.RequestIDHeader))
	}
	// The reason the request carries a query value at all: a token in a URL is a
	// real client's habit, and a trace backend is a real second place data goes.
	// The server conventions record the path and not the full URL, so no attribute
	// carries what was after the "?".
	for _, kv := range span.Attributes() {
		if v := kv.Value.AsString(); strings.Contains(v, "secret") {
			t.Errorf("%s carries %q; a query value reached the span", kv.Key, v)
		}
	}
}

// TestAnUnresolvedHostCarriesNeitherTenantKey is the honest half: an id invented
// for a tenant nobody resolved would read in a trace as a site that exists.
func TestAnUnresolvedHostCarriesNeitherTenantKey(t *testing.T) {
	router := tracedFixture(t)
	spans.Reset()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://nowhere.test/api/v1/probe/ping", nil))
	span := serverSpan(t)
	for _, key := range []string{telemetry.AttrTenant, telemetry.AttrTenantID} {
		if got, found := attributeOf(span, key); found {
			t.Errorf("a host the loader does not know was traced with %s = %s", key, got.Emit())
		}
	}
	// The same request at a host the loader does know, one path further down, is
	// refused — and the refusal is counted, by class, with no tenant attached
	// because none was ever resolved for the path. The counter and the span hold to
	// the same honesty.
	spans.Reset()
	router.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "http://"+host+"/api/v1/probe/nopath", nil))
	if total := refusalCount(t, "not_found"); total < 1 {
		t.Errorf("refusals of class not_found counted %d, want at least one", total)
	}
}

// TestADeclaredOperationRecordsItsLatency is the other number: the histogram the
// dashboard reads, named by the operation, and — the brief's whole point — carrying
// the tenant. It is recorded by the middleware that resolves the tenant, which is
// the only place in this router that holds the operation and the tenant at once.
func TestADeclaredOperationRecordsItsLatency(t *testing.T) {
	router := tracedFixture(t)
	spans.Reset()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+host+"/api/v1/probe/ping", nil))
	if rec.Code >= 400 {
		t.Fatalf("the route answered %d, want it to reach the handler", rec.Code)
	}
	var seen bool
	for _, p := range histogramPoints(t, "pkit.http.operation.duration") {
		op, ok := p.Attributes.Value(attribute.Key("pkit.operation"))
		if !ok || op.AsString() != "probe_ping" {
			continue
		}
		seen = true
		for _, key := range []string{telemetry.AttrTenant, telemetry.AttrTenantID} {
			if _, has := p.Attributes.Value(attribute.Key(key)); !has {
				t.Errorf("the latency of a tenant's request carries no %s: %v", key, p.Attributes.ToSlice())
			}
		}
	}
	if !seen {
		t.Error("no operation latency was recorded for probe_ping")
	}
}

// --- helpers ---

// serverSpan is the one span a request is. The recorder also catches the
// cross-tenant transaction the host resolver opens, which is a child and not the
// thing under test.
func serverSpan(t *testing.T) sdktrace.ReadOnlySpan {
	t.Helper()
	var found []sdktrace.ReadOnlySpan
	for _, s := range spans.Ended() {
		if s.SpanKind() == trace.SpanKindServer {
			found = append(found, s)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d server spans, want exactly one (all: %v)", len(found), spanNames(spans.Ended()))
	}
	return found[0]
}

func spanNames(all []sdktrace.ReadOnlySpan) []string {
	out := make([]string, 0, len(all))
	for _, s := range all {
		out = append(out, s.Name())
	}
	return out
}

func attributeOf(span sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// metrics returns everything collected so far. A ManualReader cannot be emptied
// and the meter provider delegates exactly once per process, so the cases read the
// running total and ask about the attribute set they care about rather than about
// a count from the start of the binary.
func metrics(t *testing.T) map[string]metricdata.Metrics {
	t.Helper()
	var out metricdata.ResourceMetrics
	if err := readings.Collect(context.Background(), &out); err != nil {
		t.Fatalf("collect: %v", err)
	}
	byName := map[string]metricdata.Metrics{}
	for _, scope := range out.ScopeMetrics {
		for _, m := range scope.Metrics {
			byName[m.Name] = m
		}
	}
	return byName
}

func histogramPoints(t *testing.T, name string) []metricdata.HistogramDataPoint[float64] {
	t.Helper()
	m, ok := metrics(t)[name]
	if !ok {
		t.Fatalf("no %s was collected", name)
	}
	h, ok := m.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("%s is %T, want a histogram", name, m.Data)
	}
	return h.DataPoints
}

func refusalCount(t *testing.T, class string) int64 {
	t.Helper()
	m, ok := metrics(t)["pkit.http.refusals"]
	if !ok {
		t.Fatal("no pkit.http.refusals was collected")
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("refusals is %T, want a sum", m.Data)
	}
	var total int64
	for _, p := range sum.DataPoints {
		if v, found := p.Attributes.Value(attribute.Key(telemetry.AttrRefusalClass)); found && v.AsString() == class {
			total += p.Value
		}
	}
	return total
}

// TestANumberCarriesNothingThatNamesOneRequest is the cardinality guard. A span
// describes one request, so the request id is the most useful thing it can carry;
// a metric attribute is part of the identity of a time series, so the same key on
// a datapoint means one series per request — a dashboard that cannot average
// anything and a backend whose index grows with traffic. Both numbers this router
// records are asked about here, on a run where every request really did carry an
// id: the exemplar is what leads from a number to a trace, and it names the span
// without becoming an attribute of the series.
func TestANumberCarriesNothingThatNamesOneRequest(t *testing.T) {
	router := tracedFixture(t)
	for _, path := range []string{"/api/v1/probe/ping", "/api/v1/probe/nopath"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+host+path, nil))
		if rec.Header().Get(httpx.RequestIDHeader) == "" {
			t.Fatalf("%s: the response carries no %s, so this case would prove nothing",
				path, httpx.RequestIDHeader)
		}
	}
	for name, m := range metrics(t) {
		var sets []attribute.Set
		switch d := m.Data.(type) {
		case metricdata.Histogram[float64]:
			for _, p := range d.DataPoints {
				sets = append(sets, p.Attributes)
			}
		case metricdata.Sum[int64]:
			for _, p := range d.DataPoints {
				sets = append(sets, p.Attributes)
			}
		case metricdata.Gauge[float64]:
			for _, p := range d.DataPoints {
				sets = append(sets, p.Attributes)
			}
		}
		for _, s := range sets {
			if _, has := s.Value(attribute.Key(telemetry.AttrRequestID)); has {
				t.Errorf("%s carries %s as a datapoint attribute (%v): a value unique to one request makes one time series per request, and the number cannot be averaged any more",
					name, telemetry.AttrRequestID, s.ToSlice())
			}
		}
	}
}
