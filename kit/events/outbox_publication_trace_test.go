package events_test

import (
	"context"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestOutboxPublicationSpanNamesItsTenantAndRequest(t *testing.T) {
	previous := otel.GetTracerProvider()
	spans := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	otel.SetTextMapPropagator(telemetry.Propagators())
	_, conn := dbtest.Schema(t)
	const name = "signal.published"
	const requestID = "request-that-published"
	ctx := telemetry.WithRequestID(tenancy.WithTenant(t.Context(), acme), requestID)
	ctx, requestSpan := telemetry.Tracer().Start(ctx, "request",
		trace.WithAttributes(telemetry.SpanAttrs(ctx)...))
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, name, map[string]any{"accepted": true})
	})
	requestSpan.End()
	if err != nil {
		t.Fatalf("publish the request's event: %v", err)
	}
	before := len(spans.Ended())
	delivery := &recorder{}
	if err := events.Relay(t.Context(), conn, delivery); err != nil {
		t.Fatalf("relay the committed event: %v", err)
	}
	if got := delivery.names(); len(got) != 1 || got[0] != name {
		t.Fatalf("relay published %v, want the committed %q", got, name)
	}
	var observed []string
	for _, span := range spans.Ended()[before:] {
		observed = append(observed, fmt.Sprintf("%s %v", span.Name(), span.Attributes()))
		attrs := attribute.NewSet(span.Attributes()...)
		id, hasID := attrs.Value(attribute.Key(telemetry.AttrTenantID))
		request, hasRequest := attrs.Value(attribute.Key(telemetry.AttrRequestID))
		if hasID && id.AsString() == acme.ID.String() &&
			hasRequest && request.AsString() == requestID {
			return
		}
	}
	t.Errorf("the relay published the row, but no publication span names tenant %s and request %q; relay spans: %v",
		acme.ID, requestID, observed)
}
