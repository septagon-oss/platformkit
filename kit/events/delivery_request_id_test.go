package events_test

// The brief's Specify item 2 names what each of the four
// boundary spans carries: "each carrying `pkit.tenant` and the request id". Three of
// them do — the request's own span, the transaction under Tx and the job's tenant
// span all read the id out of the W3C Baggage the router writes.
//
// The delivery boundary does not, and cannot as written: the outbox row stores the
// two W3C *trace* members (traceparent, tracestate) and no baggage, so when
// startDelivery extracts the publisher's context the trace id arrives and the request
// id does not. kit/telemetry's own package comment says the id is stamped on the
// spans this kernel opens "because a job run and a delivery have no request span to
// sit under" — the delivery does have one, and still has no id on it.
//
// That the delivery's span is on the publisher's trace and names its tenant is
// already pinned by the delivery's own trace_test.go; this case asks the one question
// nobody asked, and reaches it through the two facts that do work today (the handler
// ran, and its span names the tenant), never through the attribute that is missing.

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestADeliverySpanNamesTheRequestThatCausedIt(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	otel.SetTextMapPropagator(telemetry.Propagators())

	const requestID = "5a4b3c2d-1111-4222-8333-444455556666"
	const name = "billing.invoice_issued"
	_, conn := dbtest.Schema(t)

	done := make(chan struct{}, 1)
	transport := memory.New()
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	if err := events.Consume(ctx, conn, transport, []events.Subscription{{
		Module: "billing", Name: name,
		Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error {
			select {
			case done <- struct{}{}:
			default:
			}
			return nil
		},
	}}); err != nil {
		t.Fatalf("Consume: %v", err)
	}

	// The request: inside the publisher's span, with the id in its baggage exactly as
	// kit/httpx's requestID middleware leaves it.
	publishCtx := telemetry.WithRequestID(trace.ContextWithSpanContext(t.Context(), publisher), requestID)
	err := db.Run(tenancy.WithTenant(publishCtx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, name, map[string]any{"amount": 1})
	})
	if err != nil {
		t.Fatalf("publish inside the request: %v", err)
	}
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("relay it: %v", err)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the handler never ran, so no delivery span exists to ask")
	}

	var delivery sdktrace.ReadOnlySpan
	for _, s := range recorder.Ended() {
		if s.Name() == name+" deliver" {
			delivery = s
		}
	}
	if delivery == nil {
		t.Fatalf("no %q span was recorded at all (spans: %v)", name+" deliver", reviewSpanNames(recorder.Ended()))
	}

	// The two things that work today, asserted first so the case is known to have
	// reached a real delivery span before it asks for the third.
	tenantID, hasTenant := reviewAttr(delivery, telemetry.AttrTenantID)
	if !hasTenant {
		t.Fatalf("the delivery span carries no %s: %v", telemetry.AttrTenantID, delivery.Attributes())
	}
	if tenantID.AsString() != acme.ID.String() {
		t.Fatalf("%s = %s, want the event's own tenant %s",
			telemetry.AttrTenantID, tenantID.AsString(), acme.ID)
	}
	if delivery.SpanContext().TraceID() != publisher.TraceID() {
		t.Errorf("the delivery is on trace %s, want the publisher's %s",
			delivery.SpanContext().TraceID(), publisher.TraceID())
	}

	// And the brief's second half.
	id, has := reviewAttr(delivery, telemetry.AttrRequestID)
	if !has {
		t.Errorf("the delivery span carries no %s, though the brief names it for every boundary "+
			"and the request that published this event carried %q: the outbox row stores the two W3C "+
			"trace members and drops the baggage that holds the id, so an operator reading a worker's "+
			"span cannot quote the id the response header showed",
			telemetry.AttrRequestID, requestID)
	} else if id.AsString() != requestID {
		t.Errorf("%s = %s, want %q", telemetry.AttrRequestID, id.AsString(), requestID)
	}
}

// reviewAttr reads one attribute off a recorded span.
func reviewAttr(s sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

func reviewSpanNames(all []sdktrace.ReadOnlySpan) []string {
	out := make([]string, 0, len(all))
	for _, s := range all {
		out = append(out, s.Name())
	}
	return out
}
