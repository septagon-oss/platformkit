package events_test

// Review round 2 (T-0110). Review round 1's finding 2 was that the delivery span
// carried no pkit.request.id; 769d065 cures it by storing the W3C baggage member on
// the outbox row (migrations/000029) and extracting it in startDelivery. This round's
// remit is whether that cure holds, and the cure's Verified paragraph claims two
// things the case round 1 left does not test:
//
//	`startDelivery` extracts it through the same propagator, so the id lands on the
//	delivery span and, because it is the handler's context, on every transaction span
//	below it.                                             (769d065, commit body)
//
//	Not verified: a JetStream round-trip of the new envelope member — the transport
//	conformance fixtures pass, and no case here runs a real broker in another process.
//	                                                          (769d065, Not verified)
//
// The first is the reason the attribute is worth a migration: a delivery span that
// names the request is useless to whoever is reading the handler's slow transaction.
// The second is the half that decides whether the attribute survives leaving the
// process at all — and the transport this repository tests with (providers/memory)
// hands the struct over, without an encoder, so no existing case can see it.
//
// The two cases below close both. Each reaches its assertion through what already
// works — the handler ran, its span names the event's own tenant and sits on the
// publisher's trace — and never through the attribute at issue.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestTheSpansTheHandlerOpensNameTheRequestThatCausedThem is the first claim: the
// transaction the handler runs in — not the delivery span above it — carries the id.
func TestTheSpansTheHandlerOpensNameTheRequestThatCausedThem(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	otel.SetTextMapPropagator(telemetry.Propagators())

	const requestID = "1f2e3d4c-5566-4777-8888-99aabbccddeeff"
	const name = "billing.invoice_issued"
	_, conn := dbtest.Schema(t)

	ran := make(chan struct{}, 1)
	tr := memory.New()
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	if err := events.Consume(ctx, conn, tr, []events.Subscription{{
		Module: "billing", Name: name,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			// The handler's own context, and its own transaction — the span this
			// case is about opens with this transaction (see kit/db/tx.go).
			if err := tx.DB().Exec("SELECT 1").Error; err != nil {
				return err
			}
			select {
			case ran <- struct{}{}:
			default:
			}
			return nil
		},
	}}); err != nil {
		t.Fatalf("Consume: %v", err)
	}

	publishCtx := telemetry.WithRequestID(
		trace.ContextWithSpanContext(t.Context(), publisher), requestID)
	if err := db.Run(tenancy.WithTenant(publishCtx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, name, map[string]any{"amount": 1})
	}); err != nil {
		t.Fatalf("publish inside the request: %v", err)
	}
	if err := events.Relay(t.Context(), conn, tr); err != nil {
		t.Fatalf("relay it: %v", err)
	}

	// Wait for the handler's transaction span to have *ended*, which is after the
	// delivery span ends — so the recorder holds both by the time the loop exits.
	deadline := time.Now().Add(15 * time.Second)
	var txSpan, delivery sdktrace.ReadOnlySpan
	for time.Now().Before(deadline) && txSpan == nil {
		for _, s := range recorder.Ended() {
			if s.Name() == name+" deliver" {
				delivery = s
			}
			if s.Name() == "database transaction" &&
				s.SpanContext().TraceID() == publisher.TraceID() {
				txSpan = s
			}
		}
		if txSpan == nil {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if delivery == nil {
		t.Fatal("no delivery span was recorded, so the handler's transaction was never reached either")
	}
	if txSpan == nil {
		t.Fatalf("no %q span on the publisher's trace %s was recorded, so this case never got to "+
			"the assertion it exists to make (spans: %v)",
			"database transaction", publisher.TraceID(), review2Names(recorder.Ended()))
	}

	// Reachability first, off facts that work today: the transaction is the handler's,
	// and it names the event's tenant.
	if _, has := review2Attr(txSpan, telemetry.AttrTenantID); !has {
		t.Fatalf("the handler's transaction span carries no %s: %v",
			telemetry.AttrTenantID, txSpan.Attributes())
	}
	if txSpan.Parent().SpanID() != delivery.SpanContext().SpanID() {
		t.Errorf("the handler's transaction is a child of %s, want the delivery span %s",
			txSpan.Parent().SpanID(), delivery.SpanContext().SpanID())
	}

	if id, has := review2Attr(txSpan, telemetry.AttrRequestID); !has {
		t.Errorf("the handler's transaction span carries no %s, though the delivery span above it "+
			"does and the cure promises the id reaches every span below the delivery: the request "+
			"that published this event was %q, and the transaction a handler runs in is the span an "+
			"operator actually opens", telemetry.AttrRequestID, requestID)
	} else if id.AsString() != requestID {
		t.Errorf("%s on the handler's transaction span = %s, want %q",
			telemetry.AttrRequestID, id.AsString(), requestID)
	}
}

// TestTheEnvelopeThatLeavesTheProcessCarriesTheRequestMember is the second: the
// member must survive the encoding a broker carries, not only the in-process hop the
// tests use. providers/memory hands the struct over; providers/nats writes
// json.Marshal(ev) to the subject, so the envelope's own encoding is the contract
// between this kernel and every adapter, and the one no case here reached.
func TestTheEnvelopeThatLeavesTheProcessCarriesTheRequestMember(t *testing.T) {
	const requestID = "0a1b2c3d-4444-4aaa-8bbb-cdddeeff0011"
	ev := transport.Event{
		ID: uuid.New(), Name: "billing.invoice_issued", TenantID: acme.ID,
		Payload: json.RawMessage(`{"amount":1}`), At: time.Now().UTC(),
		TraceParent: "00-" + publisher.TraceID().String() + "-" +
			publisher.SpanID().String() + "-01",
		Baggage: telemetry.AttrRequestID + "=" + requestID,
	}
	body, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("the envelope does not encode: %v", err)
	}
	if !json.Valid(body) {
		t.Fatal("the envelope is not valid JSON")
	}
	var onTheWire map[string]any
	if err := json.Unmarshal(body, &onTheWire); err != nil {
		t.Fatalf("decode what the adapter would publish: %v", err)
	}
	got, has := onTheWire["baggage"]
	if !has {
		t.Fatalf("the envelope a broker carries has no \"baggage\" member (%s): the request id is "+
			"stored on the outbox row and extracted by a worker, but it does not survive leaving the "+
			"process, so the delivery span only names the request when the relay and the handler share "+
			"a process — which is the deployment the migration was written for",
			keysOf(onTheWire))
	}
	if got != ev.Baggage {
		t.Errorf("envelope baggage = %v, want %q", got, ev.Baggage)
	}

	// And the round trip: a worker's reader must get the member back.
	var back transport.Event
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("a worker's decode: %v", err)
	}
	if back.Baggage != ev.Baggage {
		t.Errorf("a round-tripped envelope carries baggage %q, want %q", back.Baggage, ev.Baggage)
	}
}

// review2Attr reads one attribute off a recorded span.
func review2Attr(s sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

func review2Names(all []sdktrace.ReadOnlySpan) []string {
	out := make([]string, 0, len(all))
	for _, s := range all {
		out = append(out, s.Name())
	}
	return out
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
