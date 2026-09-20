package httpx_test

import (
	"os"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/telemetry"
)

// spans is the recorder this binary installs as the process's provider, once.
// otelhttp resolves a tracer from the global on each request, so it would follow
// a provider installed per test; kit/events and kit/jobs bind theirs at
// initialization to whatever was installed first. Swap the provider per case and
// the kernel's instrumented packages do not all answer the same one, so this
// binary installs one recorder and each test clears it before reading.
var spans = tracetest.NewSpanRecorder()

func TestMain(m *testing.M) {
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	otel.SetTextMapPropagator(telemetry.Propagators())
	os.Exit(m.Run())
}

// fresh empties the recorder, so every span a test below finds is one that test
// made. Nothing in this package runs in parallel, so nothing else is writing.
func fresh(t *testing.T) {
	t.Helper()
	spans.Reset()
}

// attributeOf is one attribute of a finished span, and whether it was there at
// all: the difference between an absent tenant and a tenant named "" is the whole
// content of two of the assertions below.
func attributeOf(span sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// wantOneSpan is the shape every request must leave behind — exactly one server
// span — and wantAttributes then checks it against what this package promises a
// trace backend.
func wantOneSpan(t *testing.T, wanted map[string]string, secret string) sdktrace.ReadOnlySpan {
	t.Helper()
	ended := spans.Ended()
	if len(ended) != 1 {
		names := make([]string, 0, len(ended))
		for _, s := range ended {
			names = append(names, s.Name())
		}
		t.Fatalf("%d spans, want exactly one server span: %v", len(ended), names)
	}
	span := ended[0]
	if span.SpanKind() != trace.SpanKindServer {
		t.Errorf("%s is a %s span, want the server span", span.Name(), span.SpanKind())
	}
	for key, want := range wanted {
		got, found := attributeOf(span, key)
		if !found {
			t.Errorf("%s has no %s attribute", span.Name(), key)
			continue
		}
		if got.Emit() != want {
			t.Errorf("%s %s = %s, want %q", span.Name(), key, got.Emit(), want)
		}
	}
	// The reason the tests below send a query value at all: a token in a URL is a
	// real client's habit, and a trace backend is a real second reader of
	// everything recorded about a request. Nothing the request carries in a body,
	// a header or a query belongs on a span.
	if secret != "" {
		for _, kv := range span.Attributes() {
			if strings.Contains(kv.Value.Emit(), secret) {
				t.Errorf("%s leaks %q in %s=%s", span.Name(), secret, kv.Key, kv.Value.Emit())
			}
		}
	}
	return span
}

// TestOneRequestMakesOneSpanNamedForItsOperation is the request half of the
// kernel's tracing: one span, named for the operation the router resolved rather
// than for the URL the client typed, carrying the route and the tenant and the
// caller — and carrying nothing the query string brought.
func TestOneRequestMakesOneSpanNamedForItsOperation(t *testing.T) {
	api, router, f := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "read-widget", Method: "GET", Path: "/widgets/{id}",
	}, httpx.Permission("widget:read"), ok)
	f.allow = true
	f.signedIn()
	fresh(t)

	w := get(t, router, "/widgets/7?token=a-secret-in-a-url")
	if w.Code != 200 {
		t.Fatalf("the request was answered %d", w.Code)
	}
	span := wantOneSpan(t, map[string]string{
		"http.request.method":       "GET",
		"http.route":                "/widgets/{id}",
		"platformkit.tenant":        f.tenant.Slug,
		"enduser.id":                f.principal.UserID.String(),
		"http.response.status_code": "200",
	}, "a-secret-in-a-url")
	if span.Name() != "read-widget" {
		t.Errorf("span name = %q, want the operation id and not the URL the client typed", span.Name())
	}
}

// TestAnAnonymousRequestCarriesNoCaller, and a host that names no site carries no
// tenant either: an id invented for a request that resolved nothing would read in
// a trace as a tenant that exists.
func TestAnAnonymousRequestCarriesNoCaller(t *testing.T) {
	api, router, _ := setup(t)
	// A public operation, so the request reaches the handler with no caller at
	// all: a permissioned one would answer 403 first, which is the right answer
	// and says nothing about the span.
	httpx.Register(api, huma.Operation{
		OperationID: "public-page", Method: "GET", Path: "/public",
	}, httpx.Public(), ok)
	fresh(t)

	if w := get(t, router, "/public"); w.Code != 200 {
		t.Fatalf("the request was answered %d", w.Code)
	}
	span := wantOneSpan(t, map[string]string{"platformkit.tenant": "acme"}, "")
	if _, found := attributeOf(span, "enduser.id"); found {
		t.Error("an anonymous request carries an enduser.id")
	}

	// The same router and the same public operation on a host the loader does not
	// know. The span says nothing about a tenant, because there is none to say:
	// an id invented here would read as a site that exists.
	fresh(t)
	if w := request(t, router, "GET", "/public", "elsewhere.test"); w.Code != 200 {
		t.Fatalf("the public request was answered %d", w.Code)
	}
	for _, span := range spans.Ended() {
		if _, found := attributeOf(span, "platformkit.tenant"); found {
			t.Errorf("%s names a tenant for a host that resolved to none", span.Name())
		}
	}
}

// TestAnUnmatchedRequestIsStillSpanned: a 404 is the request a reader of a trace
// most wants to see, and it never reaches an operation, so it keeps otelhttp's
// own name and carries no route.
func TestAnUnmatchedRequestIsStillSpanned(t *testing.T) {
	_, router, _ := setup(t)
	fresh(t)
	if w := get(t, router, "/nothing/here"); w.Code != 404 {
		t.Fatalf("want 404, got %d", w.Code)
	}
	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("%d spans, want one", len(ended))
	}
	if _, found := attributeOf(ended[0], "http.route"); found {
		t.Errorf("a request that matched no route carries http.route %v", ended[0].Attributes())
	}
	if ended[0].Name() != "GET" {
		t.Errorf("span name = %q, want the method alone", ended[0].Name())
	}
}
