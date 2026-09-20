package events_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	provider "github.com/septagon-oss/platformkit/kit/events/providers/nats"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// spans is the recorder this binary installs as the process's provider, once. A
// Tracer made before the first install stays bound to the provider that first
// install named, and this package made its tracer at initialization, so a test
// that installed a provider of its own per case would read spans that went
// somewhere else. One recorder for the whole binary, cleared per test and named
// per test, is what every span lands in.
var spans = tracetest.NewSpanRecorder()

func TestMain(m *testing.M) {
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	// The propagator is what leaves a context on the outbox row and what reads it
	// back on the way in, so a test of continuity has to install the same one a
	// composition root installs. See kit/telemetry.
	otel.SetTextMapPropagator(telemetry.Propagators())
	os.Exit(m.Run())
}

// fresh empties the recorder. Nothing in this package runs in parallel; a
// delivery left retrying by an earlier test is a delivery of an event named for
// that test, and no name below is one of them.
func fresh(t *testing.T) {
	t.Helper()
	spans.Reset()
}

// ended waits for one span by name. Delivery happens on the transport's own
// goroutine, so the span arrives when the handler finishes rather than when the
// test looks; what is not negotiable is that it arrives.
func ended(t *testing.T, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, span := range spans.Ended() {
			if span.Name() == name {
				return span
			}
		}
		if time.Now().After(deadline) {
			var saw []string
			for _, s := range spans.Ended() {
				saw = append(saw, s.Name())
			}
			t.Fatalf("no span named %q; saw %v", name, saw)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func wantAttribute(t *testing.T, span sdktrace.ReadOnlySpan, key, want string) {
	t.Helper()
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			if kv.Value.Emit() != want {
				t.Errorf("%s %s = %s, want %q", span.Name(), key, kv.Value.Emit(), want)
			}
			return
		}
	}
	t.Errorf("%s has no %s attribute", span.Name(), key)
}

// TestADeliveryContinuesTheTraceThatPublishedIt is the reason the trace context
// is stored on the outbox row: the request that moved the state and the handler
// that reacted to it — hours, or another process, later — are one trace, and the
// reader of the trace does not have to know that a database and a relay were in
// between. The relay's own span says what the pass moved, which is the only
// question a reader has of a relay.
func TestADeliveryContinuesTheTraceThatPublishedIt(t *testing.T) {
	_, conn := dbtest.Schema(t)
	fresh(t)
	transport := memory.New()
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}

	delivered := make(chan events.Event, 1)
	err := events.Consume(t.Context(), conn, transport, []events.Subscription{{
		Module: "tracing", Name: "tracing.published",
		Handler: func(_ context.Context, _ db.Tx[db.Tenant], ev events.Event) error {
			delivered <- ev
			return nil
		},
	}})
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	pctx, publisher := otel.Tracer("events.test").Start(t.Context(), "publish note")
	err = db.Run(tenancy.WithTenant(pctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, "tracing.published", nil)
	})
	publisher.End()
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("Relay: %v", err)
	}

	var deliveredTo events.Event
	select {
	case deliveredTo = <-delivered:
	case <-time.After(10 * time.Second):
		t.Fatal("the event was never delivered")
	}

	delivery := ended(t, "tracing.published deliver")
	if got, want := delivery.Parent(), publisher.SpanContext(); got.TraceID() != want.TraceID() || got.SpanID() != want.SpanID() {
		t.Errorf("the delivery's parent is %v, want the publishing span %v", got, want)
	}
	if !delivery.Parent().IsRemote() {
		t.Error("the parent of a delivery is a span in another process's work, which is remote by definition")
	}
	wantAttribute(t, delivery, "messaging.system", "memory")
	// The id the publisher minted, which the handler saw and the span names: the
	// one handle that joins a trace to the outbox row and to the claim.
	wantAttribute(t, delivery, "messaging.message.id", deliveredTo.ID.String())
	wantAttribute(t, delivery, "messaging.destination.name", "tracing.published")
	wantAttribute(t, delivery, "platformkit.tenant", tenant.ID.String())
	wantAttribute(t, delivery, "platformkit.events.attempt", "1")

	batch := ended(t, "outbox relay batch")
	wantAttribute(t, batch, "platformkit.events.relayed", "1")
}

// TestAFailedHandlerMarksItsSpan: a delivery that failed has to be findable in a
// trace, because the reason it failed is the question the span exists for.
func TestAFailedHandlerMarksItsSpan(t *testing.T) {
	_, conn := dbtest.Schema(t)
	fresh(t)
	transport := memory.New()
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	poison := errors.New("this handler cannot work")

	if err := events.Consume(t.Context(), conn, transport, []events.Subscription{{
		Module: "tracing", Name: "tracing.failing",
		Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error { return poison },
	}}); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, "tracing.failing", nil)
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("Relay: %v", err)
	}

	delivery := ended(t, "tracing.failing deliver")
	if status := delivery.Status(); status.Code != codes.Error || status.Description != poison.Error() {
		t.Errorf("the failed delivery is %v, want an error status saying %q", status, poison)
	}
	if len(delivery.Events()) == 0 {
		t.Error("the failed delivery recorded no event beside its status")
	}
}

// TestAnUntracedPublishStillMakesASpan: a periodic job has no request to hang
// off, and its delivery is a trace of its own rather than nothing at all.
func TestAnUntracedPublishStillMakesASpan(t *testing.T) {
	_, conn := dbtest.Schema(t)
	fresh(t)
	transport := memory.New()
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	done := make(chan struct{})

	if err := events.Consume(t.Context(), conn, transport, []events.Subscription{{
		Module: "tracing", Name: "tracing.scheduled",
		Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error { close(done); return nil },
	}}); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, "tracing.scheduled", nil)
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("Relay: %v", err)
	}
	<-done

	delivery := ended(t, "tracing.scheduled deliver")
	if delivery.Parent().IsValid() {
		t.Errorf("a publish with no span has a parent %v", delivery.Parent())
	}
	if !delivery.SpanContext().IsValid() || delivery.SpanContext().TraceID() == delivery.Parent().TraceID() {
		t.Error("the delivery did not start a trace of its own")
	}
	wantAttribute(t, delivery, "platformkit.events.attempt", "1")
}

// TestTraceContextSurvivesJetStream is the same claim as the one above, on the
// transport a separated worker actually uses. The in-memory transport hands the
// handler the event it was given, so it cannot test the trip the context takes:
// two outbox columns, two CloudEvents extension attributes, a broker, and back to a
// parent. A change proven only on memory is not proven on the path `web` and
// `worker` roles take.
func TestTraceContextSurvivesJetStream(t *testing.T) {
	url := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if url == "" {
		t.Fatal("PLATFORMKIT_TEST_NATS_URL is unset; start the stack with `make up`")
	}
	_, conn := dbtest.Schema(t)
	fresh(t)
	// The stream is shared with every other run, so the name is this run's alone.
	name := "trace" + strings.ReplaceAll(uuid.NewString()[:8], "-", "") + ".happened"
	transport, err := provider.JetStream(url)
	if err != nil {
		t.Fatalf("JetStream(%s): %v", url, err)
	}
	closer, ok := transport.(interface{ Close() error })
	if !ok {
		t.Fatal("the JetStream transport does not close its connection")
	}
	t.Cleanup(func() { _ = closer.Close() })

	delivered := make(chan events.Event, 1)
	if err := events.Consume(t.Context(), conn, transport, []events.Subscription{{
		Module: "trace", Name: name,
		Handler: func(_ context.Context, _ db.Tx[db.Tenant], ev events.Event) error {
			delivered <- ev
			return nil
		},
	}}); err != nil {
		t.Fatalf("Consume: %v", err)
	}

	pctx, publisher := otel.Tracer("events.test").Start(t.Context(), "publish")
	err = db.Run(tenancy.WithTenant(pctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, name, map[string]any{"amount": 42})
	})
	publisher.End()
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("Relay: %v", err)
	}
	select {
	case ev := <-delivered:
		// What the broker handed back, before the span is read: the envelope is
		// the only carrier here, so an empty field means it was lost on the wire.
		if ev.TraceParent == "" {
			t.Error("the event came back from JetStream with no trace context")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("nothing came back from JetStream")
	}

	delivery := ended(t, name+" deliver")
	if got, want := delivery.Parent(), publisher.SpanContext(); got.TraceID() != want.TraceID() || got.SpanID() != want.SpanID() {
		t.Errorf("the delivery's parent is %v, want the publishing span %v", got, want)
	}
	wantAttribute(t, delivery, "messaging.system", "nats")
	// The broker's own count, not a counter this process keeps: on a first
	// delivery it is one, and on the fourth redelivery it is four.
	wantAttribute(t, delivery, "platformkit.events.attempt", "1")
}
