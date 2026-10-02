package events_test

// The trace context an event carries, from the transaction that published it to the
// handler that reacts to it.
//
// The outbox row is where the two halves meet: the publisher commits in one process
// and the relay publishes in another, possibly one that started hours later, so the
// claim "the request and the reaction are one trace" has to be checked in three
// places — on the row, on the envelope the relay hands the transport, and in the
// context the handler is handed. This file checks all three.
//
// No collector and no SDK provider is involved. What is asserted is the W3C context
// this package writes and reads, through the propagator kit/telemetry names, which is
// the whole of what kit/events can promise. The span itself is read off the SDK's
// recorder in kit/httpx/tracing_test.go, and a deployment's backend is another
// question again — the brief's own acceptance is the recorder, not a collector.

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// publisher is the span the request that published the event was inside, named once
// so the row, the envelope and the handler's context are each checked against the same
// sixteen bytes rather than against three copies of a literal.
var publisher = trace.NewSpanContext(trace.SpanContextConfig{
	TraceID:    trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36},
	SpanID:     trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
	TraceFlags: trace.FlagsSampled,
})

// publisherTraceparent is what W3C spells that context as, and what all three places
// above have to agree on.
func publisherTraceparent() string {
	return "00-" + publisher.TraceID().String() + "-" + publisher.SpanID().String() + "-01"
}

// publishInsideTrace writes one event from a transaction whose context names the
// publisher's span, which is the shape a request handler is in.
func publishInsideTrace(t *testing.T, conn *db.Conn, name string) {
	t.Helper()
	ctx := trace.ContextWithSpanContext(t.Context(), publisher)
	err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, name, map[string]any{"amount": 1})
	})
	if err != nil {
		t.Fatalf("publish %s inside a span: %v", name, err)
	}
}

// TestThePublishersTraceContextIsStoredOnTheOutboxRow is the half that makes the rest
// possible: the row carries the trace it was written inside, in the transaction that
// wrote it. The columns are nullable because an untraced publisher — a periodic job, a
// deployment with no collector — has nothing to leave, and NULL is the honest answer.
// A publisher that did have one must not lose it, because the row is the only thing
// that outlives the request.
func TestThePublishersTraceContextIsStoredOnTheOutboxRow(t *testing.T) {
	otel.SetTextMapPropagator(telemetry.Propagators())
	_, conn := dbtest.Schema(t)

	publishInsideTrace(t, conn, "billing.invoice_issued")

	var got string
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT COALESCE(traceparent, '') FROM platformkit_outbox`).Row().Scan(&got)
	})
	if err != nil {
		t.Fatalf("read the row back: %v", err)
	}
	if want := publisherTraceparent(); got != want {
		t.Fatalf("the outbox row carries traceparent %q, want the publisher's %q", got, want)
	}
}

// TestTheTraceContinuesIntoTheDelivery is the same context on the other side of the
// relay: the envelope names the publisher, and the handler is handed a context on that
// trace rather than at the start of a new one. A handler that started its own trace
// would leave an operator with two unrelated traces and no way to join them — which is
// the failure this case exists to catch, and which no other test in this repository
// could see, because nothing else crosses the database, the relay and the broker.
func TestTheTraceContinuesIntoTheDelivery(t *testing.T) {
	otel.SetTextMapPropagator(telemetry.Propagators())
	_, conn := dbtest.Schema(t)

	type delivered struct {
		envelope string
		scope    trace.SpanContext
	}
	seen := make(chan delivered, 4)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	transport := memory.New()
	subs := []events.Subscription{{
		Module: "billing", Name: "billing.invoice_issued",
		Handler: func(ctx context.Context, _ db.Tx[db.Tenant], ev events.Event) error {
			select {
			case seen <- delivered{envelope: ev.TraceParent, scope: trace.SpanContextFromContext(ctx)}:
			default:
			}
			return nil
		},
	}}
	if err := events.Consume(ctx, conn, transport, subs); err != nil {
		t.Fatalf("Consume: %v", err)
	}

	publishInsideTrace(t, conn, "billing.invoice_issued")
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("Relay: %v", err)
	}

	var got delivered
	select {
	case got = <-seen:
	case <-time.After(10 * time.Second):
		t.Fatal("the handler never ran, so the trace never crossed the relay")
	}
	if want := publisherTraceparent(); got.envelope != want {
		t.Errorf("the envelope handed the transport carries %q, want %q", got.envelope, want)
	}
	if got.scope.TraceID() != publisher.TraceID() {
		t.Errorf("the handler was handed trace %s, want the publisher's %s: the request that moved "+
			"the state and the handler that reacted to it are two traces", got.scope.TraceID(), publisher.TraceID())
	}
}

// The write's half of the same promise: a publisher with no span to leave behind
// stores nothing, rather than the empty string the propagator answers for an absent
// member. migrations/000028 and 000036 both say NULL is the ordinary case, and
// `WHERE traceparent IS NULL` is how a person asks the table which events were never
// traced — a question the empty string answers with an empty result, and a row that
// looks like a traced publish whose header was corrupted.
func TestAnUntracedPublishLeavesNoTraceContextOnItsRow(t *testing.T) {
	_, conn := dbtest.Schema(t)
	publish(t, conn, acme, "billing.invoice_issued", map[string]any{"amount": 1})

	var rows, withParent, withBaggage int
	var parent *string
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Raw(`SELECT count(*), count(traceparent), count(baggage), max(traceparent) `+
				`FROM platformkit_outbox`).Row().Scan(&rows, &withParent, &withBaggage, &parent)
		})
	if err != nil {
		t.Fatalf("read the row back: %v", err)
	}
	if rows != 1 {
		t.Fatalf("the queue holds %d rows, want the one this case published", rows)
	}
	if withParent != 0 || withBaggage != 0 {
		t.Errorf("an untraced publish stored traceparent on %d rows and baggage on %d, want neither; "+
			"the write stores an absent member as NULL (absentAsNull), and the empty string would make "+
			"`traceparent IS NULL` miss every row written after migrations/000028 (got %q)",
			withParent, withBaggage, deref(parent))
	}
}

func deref(s *string) string {
	if s == nil {
		return "<NULL>"
	}
	return *s
}
