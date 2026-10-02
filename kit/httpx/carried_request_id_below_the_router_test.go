package httpx_test

// The request id a span below the router names must be the id the caller was
// answered with.
//
// kit/telemetry's package comment promises the join an operator performs: the
// request id "is *stamped* as well, on the spans this kernel opens itself, because
// a job run has no request span to sit under and an operator joins a log line to a
// trace by quoting the id the response header carried". kit/httpx/request_id.go
// puts that id in W3C Baggage, and kit/telemetry.RequestID reads it back out of the
// context wherever a child span is opened — kit/db's "database transaction" span
// (tx.go), kit/jobs' run span and kit/events' delivery span all stamp it through
// telemetry.SpanAttrs — and kit/events' carriedContext injects the same bag onto the
// outbox row's baggage column (migrations/000036), which is how the id crosses into
// the worker's spans.
//
// Baggage is a *request* header this router extracts: the composite propagator
// telemetry.Propagators() — which kit/app installs unconditionally, before it looks
// at telemetry.otlp_endpoint — contains propagation.Baggage{}. So the bag a caller
// sent is already in the context when request_id.go writes into it. When the id the
// caller sent is one Baggage will not carry (givenID accepts any printable ASCII to
// 64 bytes; the W3C baggage-octet set excludes only ",", ";" and DQUOTE),
// telemetry.WithRequestID returns the context untouched, and the caller's own
// pkit.request.id is then the only one below the router. The request's own span is
// stamped from the id the response carried, so the parent reads correctly while
// every span and row beneath it names a request nobody was answered for.
//
// Reachability here is the answer the client got — a status and an X-Request-ID —
// and the attributes and members this process emits, all of which the fixed
// behaviour produces as well.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// carriedIDTenant is this fixture's tenant, under an id no other case in this
// binary moves.
var carriedIDTenant = uuid.MustParse("1c11c111-1c11-41c1-81c1-1c11c111c111")

const (
	carriedIDHost   = "carried-id.test"
	carriedIDForged = "the-id-the-caller-invented"
	// carriedIDGiven is the id the caller asks to be answered under. givenID keeps
	// it — every byte is printable ASCII — and baggage.NewMember refuses it: the
	// W3C baggage-octet set excludes ",", ";" and DQUOTE, and nothing else. That
	// disagreement is the case: an id this kernel answers in a header is an id it
	// cannot carry in a bag, and the bag the caller sent is left in its place.
	carriedIDGiven = "carried,id,here"
)

// carriedBaggage records, per request, what a publisher below this handler would
// inject as its `baggage` member — the same call kit/events' carriedContext makes
// when it writes the outbox row. The handler fills it; the assertions read it after
// the response settled.
var carriedBaggage sync.Map

// carriedIDFixture serves one public operation that behaves like a handler this
// kernel ships: it opens a child span the way kit/db opens its transaction span
// (telemetry.Tracer plus telemetry.SpanAttrs) and notes the members a publisher
// would leave behind.
func carriedIDFixture(t *testing.T) http.Handler {
	t.Helper()
	_, app := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: carriedIDHost,
		Conn:       app,
		Cache:      cache.Memory("pkit"),
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
			if h != carriedIDHost {
				return tenancy.Tenant{}, tenancy.ErrNoSuchHost
			}
			return tenancy.Tenant{ID: carriedIDTenant, Slug: "carried", Name: "Carried"}, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return true, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	httpx.Register(api.Surfaces("carried").App, huma.Operation{
		OperationID: "carried_id_ping", Method: http.MethodGet, Path: "/ping",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		carrier := propagation.MapCarrier{}
		otel.GetTextMapPropagator().Inject(ctx, carrier)
		carriedBaggage.Store(t.Name(), carrier["baggage"])
		_, span := telemetry.Tracer().Start(ctx, "child of the request",
			trace.WithAttributes(telemetry.SpanAttrs(ctx)...))
		span.End()
		return &struct{}{}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the routes do not declare themselves: %v", err)
	}
	return router
}

// carriedRequest is one request at the fixture's host, answered as the client saw
// it.
func carriedRequest(t *testing.T, router http.Handler, header [][2]string) *httptest.ResponseRecorder {
	t.Helper()
	carriedBaggage.Delete(t.Name())
	spans.Reset()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://"+carriedIDHost+"/api/v1/carried/ping", nil)
	for _, h := range header {
		req.Header.Set(h[0], h[1])
	}
	router.ServeHTTP(rec, req)
	return rec
}

// carriedChild returns the span the handler opened, so a case fails on the child
// and not on the router's own span, which is stamped where the id is learned.
func carriedChild(t *testing.T) sdktrace.ReadOnlySpan {
	t.Helper()
	var (
		child sdktrace.ReadOnlySpan
		count int
	)
	for _, s := range spans.Ended() {
		if s.Name() != "child of the request" {
			continue
		}
		count++
		child = s
	}
	if count != 1 {
		t.Fatalf("%d child spans recorded, want the one this request opened", count)
	}
	return child
}

// TestAChildSpanNamesTheRequestTheCallerWasAnsweredFor is the one hop below the
// request's own span: whatever this process opens under a request names the request
// the caller holds, and never a value that caller's Baggage header supplied.
func TestAChildSpanNamesTheRequestTheCallerWasAnsweredFor(t *testing.T) {
	router := carriedIDFixture(t)

	rec := carriedRequest(t, router, [][2]string{
		{httpx.RequestIDHeader, carriedIDGiven},
		{"Baggage", telemetry.AttrRequestID + "=" + carriedIDForged},
	})

	answered := rec.Header().Get(httpx.RequestIDHeader)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("the public route answered %d, want it to reach the handler: %s", rec.Code, rec.Body.String())
	}
	if answered != carriedIDGiven {
		t.Fatalf("%s = %q, want the id the caller sent, which kit/httpx's givenID accepts",
			httpx.RequestIDHeader, answered)
	}

	child := carriedChild(t)
	// Naming no request at all is a permitted answer: migrations/000036 says a row
	// is NULL for "a request whose id Baggage itself will not carry", and a span
	// with no correlation attribute is the same fact on the other channel. What is
	// not permitted is naming one this caller wrote into a header.
	id, found := attributeOf(child, telemetry.AttrRequestID)
	if !found {
		t.Logf("the child span names no request: the caller was answered %s and Baggage will not carry it, "+
			"which is the absent-not-wrong answer the baggage column documents", answered)
	} else if id.Emit() != answered {
		t.Errorf("a span this process opened under the request names %s=%s as its cause, while the caller was "+
			"answered %s: an operator quoting the id in the response finds no span, and the span they do find "+
			"names a request nobody was answered for",
			telemetry.AttrRequestID, id.Emit(), answered)
	}
	if _, has := attributeOf(child, telemetry.AttrTenantID); !has {
		t.Errorf("the child span carries no %s, so it belongs to no tenant", telemetry.AttrTenantID)
	}
}

// TestTheContextAPublisherLeavesBehindNamesTheRequestTheCallerWasAnsweredFor is the
// same value on the channel that crosses processes: the `baggage` member the relay
// stores on the outbox row and the worker extracts to open its delivery span. A
// carried id that is not the one the caller was answered for does not stop at this
// process — migrations/000036 gives it a column and kit/events gives it the
// delivery span and every span the handler opens below it.
func TestTheContextAPublisherLeavesBehindNamesTheRequestTheCallerWasAnsweredFor(t *testing.T) {
	router := carriedIDFixture(t)

	rec := carriedRequest(t, router, [][2]string{
		{httpx.RequestIDHeader, carriedIDGiven},
		{"Baggage", telemetry.AttrRequestID + "=" + carriedIDForged},
	})

	answered := rec.Header().Get(httpx.RequestIDHeader)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("the public route answered %d, want it to reach the handler: %s", rec.Code, rec.Body.String())
	}
	v, ok := carriedBaggage.Load(t.Name())
	if !ok {
		t.Fatal("the handler never ran, so this case had no publisher context to read")
	}
	baggage, _ := v.(string)
	if carriedForgedIn(baggage) {
		t.Errorf("the baggage a publisher leaves behind is %q, which names %s — an id this caller invented, "+
			"not the %q it was answered: migrations/000036 stores it, and kit/events reads it back onto the "+
			"delivery span in the worker process, which serves this installation's tenants",
			baggage, carriedIDForged, answered)
	}
}

// carriedForgedIn reports a carried member naming the id this caller invented.
func carriedForgedIn(baggage string) bool {
	return strings.Contains(baggage, carriedIDForged)
}

// TestTheCarriedBaggageNamesNoTenantWhateverTheRequestResolved keeps the tenant
// half of the same channel pinned at the child: the caller's bag names a tenant,
// the resolver answers another, and only the resolver's may stand on a span.
func TestTheCarriedBaggageNamesNoTenantWhateverTheRequestResolved(t *testing.T) {
	router := carriedIDFixture(t)

	rec := carriedRequest(t, router, [][2]string{
		{"Baggage", "pkit.tenant=globex-carried,pkit.tenant.id=0c0c0c0c-0c0c-4c0c-8c0c-0c0c0c0c0c0c"},
	})

	if rec.Code != http.StatusNoContent {
		t.Fatalf("the public route answered %d, want it to reach the handler: %s", rec.Code, rec.Body.String())
	}
	child := carriedChild(t)
	for _, kv := range child.Attributes() {
		if v := kv.Value.Emit(); v == "globex-carried" || v == "0c0c0c0c-0c0c-4c0c-8c0c-0c0c0c0c0c0c" {
			t.Errorf("the child span carries %s=%s — a tenant this caller named in a header and the resolver "+
				"never answered", kv.Key, v)
		}
	}
	if slug, has := attributeOf(child, telemetry.AttrTenant); !has || slug.Emit() != "carried" {
		t.Errorf("%s = %v (found %v), want the tenant the host resolved", telemetry.AttrTenant, slug, has)
	}
	if v, ok := carriedBaggage.Load(t.Name()); ok {
		if baggage, _ := v.(string); strings.Contains(baggage, "globex-carried") {
			t.Errorf("a publisher below this request would leave %q on its outbox row, carrying a tenant "+
				"nobody resolved", baggage)
		}
	}
}
