package events_test

import (
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAnUntracedPublicationNamesNoUnrelatedRequest(t *testing.T) {
	previous := otel.GetTracerProvider()
	spans := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	otel.SetTextMapPropagator(telemetry.Propagators())

	_, conn := dbtest.Schema(t)
	owner := tenancy.Tenant{ID: uuid.New(), Slug: "owner"}
	publish(t, conn, owner, "signal.untraced", map[string]any{"accepted": true})

	const unrelatedRequest = "another-tenants-request"
	relayContext := telemetry.WithRequestID(tenancy.WithTenant(t.Context(), acme), unrelatedRequest)
	delivery := &recorder{}
	if err := events.Relay(relayContext, conn, delivery); err != nil {
		t.Fatalf("relay the committed event: %v", err)
	}
	if got := delivery.names(); len(got) != 1 || got[0] != "signal.untraced" {
		t.Fatalf("relay delivered %v, want the committed event", got)
	}
	if got := delivery.got[0].RequestID; got != "" {
		t.Fatalf("the delivered event has request %q, want none", got)
	}

	var publications int
	for _, span := range spans.Ended() {
		if span.Name() != "signal.untraced publish" {
			continue
		}
		publications++
		attrs := attribute.NewSet(span.Attributes()...)
		tenant, ok := attrs.Value(attribute.Key(telemetry.AttrTenantID))
		if !ok || tenant.AsString() != owner.ID.String() {
			t.Errorf("publication tenant = %q, want the event owner's %s", tenant.AsString(), owner.ID)
		}
		if request, ok := attrs.Value(attribute.Key(telemetry.AttrRequestID)); ok {
			t.Errorf("an event with no request inherited the relay caller's request %q", request.AsString())
		}
	}
	if publications != 1 {
		t.Errorf("publication spans = %d, want one for the delivered event", publications)
	}
}
