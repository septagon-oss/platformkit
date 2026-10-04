package httpx_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
)

func TestOpaqueRequestIDLeavesATraceOnItsEvent(t *testing.T) {
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(noop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	const host = "opaque-trace.test"
	const requestID = "proxy-request-opaque"
	const eventName = "signal.accepted"
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "opaque", Name: "Opaque"}
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host,
		Conn:       conn,
		Cache:      cache.Memory("pkit"),
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], got string) (tenancy.Tenant, error) {
			if got != host {
				return tenancy.Tenant{}, tenancy.ErrNoSuchHost
			}
			return tenant, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return true, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	httpx.Register(api.Surfaces("signal").App, huma.Operation{
		OperationID: "signal_accept", Method: http.MethodPost, Path: "/accept",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, errors.New("request has no tenant transaction")
		}
		return &struct{}{}, events.Publish(ctx, tx, eventName, map[string]any{"accepted": true})
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("declare the operation: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/v1/signal/accept", nil)
	req.Header.Set(httpx.RequestIDHeader, requestID)
	router.ServeHTTP(rec, req)
	if rec.Code < 200 || rec.Code >= 300 || rec.Header().Get(httpx.RequestIDHeader) != requestID {
		t.Fatalf("operation answered %d with request id %q, want success under %q: %s",
			rec.Code, rec.Header().Get(httpx.RequestIDHeader), requestID, rec.Body.String())
	}

	var count int
	var parent string
	var storedID string
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*), COALESCE(max(traceparent), ''), COALESCE(max(request_id), '')
			FROM platformkit_outbox WHERE name = ?`, eventName).Row().Scan(&count, &parent, &storedID)
	})
	if err != nil || count != 1 {
		t.Fatalf("the event committed under the request's tenant: count=%d err=%v", count, err)
	}
	if storedID != requestID {
		t.Fatalf("the committed event belongs to request %q, want the HTTP request %q", storedID, requestID)
	}
	if _, ok := trace.Parse(parent, ""); !ok {
		t.Errorf("a committed event caused by the accepted request has traceparent %q; its envelope needs a valid trace id even when the provider is off", parent)
	}

	delivered := make(chan events.Event, 1)
	transport := memory.New()
	if err := transport.Subscribe(t.Context(), "signal-opaque-trace", eventName, events.Sink{
		Handle: func(_ context.Context, ev events.Event) error {
			delivered <- ev
			return nil
		},
		Dead: func(context.Context, events.Event, error) error {
			return errors.New("the event was not handled")
		},
	}); err != nil {
		t.Fatalf("subscribe to the committed event: %v", err)
	}
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("relay the committed event: %v", err)
	}
	select {
	case ev := <-delivered:
		if _, ok := trace.Parse(ev.TraceParent, ""); !ok {
			t.Errorf("the envelope delivered for request %q has traceparent %q, want a valid W3C trace id", requestID, ev.TraceParent)
		}
	default:
		t.Fatal("the relay returned without delivering the committed event")
	}
}
