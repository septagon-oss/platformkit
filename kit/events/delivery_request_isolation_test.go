package events_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAnUntracedDeliveryNamesNoUnrelatedRequest(t *testing.T) {
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	spans := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	otel.SetTextMapPropagator(telemetry.Propagators())
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})

	_, conn := dbtest.Schema(t)
	owner := tenancy.Tenant{ID: uuid.New(), Slug: "owner"}
	const unrelatedRequest = "another-tenants-request"
	subscriptionContext := telemetry.WithRequestID(tenancy.WithTenant(t.Context(), acme), unrelatedRequest)
	transport := memory.New()
	type handled struct {
		event     events.Event
		tenant    tenancy.Tenant
		requestID string
	}
	seen := make(chan handled, 1)
	if err := events.Consume(subscriptionContext, conn, transport, []events.Subscription{{
		Module: "signal", Name: "signal.untraced",
		Handler: func(ctx context.Context, _ db.Tx[db.Tenant], ev events.Event) error {
			tenant, _ := tenancy.FromContext(ctx)
			seen <- handled{event: ev, tenant: tenant, requestID: telemetry.RequestID(ctx)}
			return nil
		},
	}}); err != nil {
		t.Fatalf("subscribe to the event: %v", err)
	}
	publish(t, conn, owner, "signal.untraced", map[string]any{"accepted": true})
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("relay the committed event: %v", err)
	}

	var got handled
	select {
	case got = <-seen:
	default:
		t.Fatal("the committed event was not delivered")
	}
	if got.event.Name != "signal.untraced" || got.tenant.ID != owner.ID {
		t.Fatalf("delivery = %q in tenant %s, want the owner's event", got.event.Name, got.tenant.ID)
	}
	if got.event.RequestID != "" || got.requestID != "" {
		t.Errorf("an event with no request carried envelope %q and handler request %q", got.event.RequestID, got.requestID)
	}

	var deliveries int
	for _, span := range spans.Ended() {
		if span.Name() != "signal.untraced deliver" {
			continue
		}
		deliveries++
		attrs := attribute.NewSet(span.Attributes()...)
		tenant, ok := attrs.Value(attribute.Key(telemetry.AttrTenantID))
		if !ok || tenant.AsString() != owner.ID.String() {
			t.Errorf("delivery tenant = %q, want the event owner's %s", tenant.AsString(), owner.ID)
		}
		if request, ok := attrs.Value(attribute.Key(telemetry.AttrRequestID)); ok {
			t.Errorf("an event with no request borrowed the subscriber's request %q", request.AsString())
		}
	}
	if deliveries != 1 {
		t.Errorf("delivery spans = %d, want one for the handled event", deliveries)
	}
}
