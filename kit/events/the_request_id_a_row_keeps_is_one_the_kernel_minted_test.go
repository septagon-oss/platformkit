package events_test

// The outbox row is where a request id outlives the process that minted it, so it is
// where a correlation defect stops being cosmetic: migrations/000041 gives the bag a
// column, the relay reads that column, and `startDelivery` extracts it into the
// context a *worker* opens its delivery span on — along with every span the handler's
// transaction opens below it. A member that reached the row would name its request in
// a process that never saw the request.
//
// Two writings of that channel are pinned here, and the second is the reason the
// first means anything:
//
//   - an id W3C Baggage will not carry — kit/httpx answers any printable ASCII a
//     client sends, and a comma is inside that range — leaves the row no member, so
//     the delivery names no request rather than naming one nobody was answered for;
//   - an id Baggage does carry arrives on the row and on the delivery span.
//
// Both requests carry a `Baggage:` header of their own, forged with the kernel's key
// and with a tenant that is not the publisher's, because that is the class of input
// the second case exists to make visible: with a bag written whole it is inert, and
// the day it is not, the row and the worker's spans are where it shows. The tenant of
// the row is asked of every span below the delivery in both cases, since it is the
// thing a wrong member would be worth to whoever sent it.

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const carriedEvent = "billing.invoice_carried"

// carriedForged is what the caller's own `Baggage:` header writes into the kernel's
// key, and a tenant of another customer entirely.
const carriedForged = "the-id-the-caller-invented"

var carriedClaimed = "globex"

// publishUnderACarriedBag runs one request-shaped publish end to end — the caller's
// bag, the answered id, the tenant transaction, the row, the relay and the delivery —
// and hands back what the row stored and what the two spans below the relay recorded.
func publishUnderACarriedBag(t *testing.T, answered string) (string, sdktrace.ReadOnlySpan, sdktrace.ReadOnlySpan) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	otel.SetTextMapPropagator(telemetry.Propagators())
	_, conn := dbtest.Schema(t)

	tr := memory.New()
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	if err := events.Consume(ctx, conn, tr, []events.Subscription{{
		Module: "billing", Name: carriedEvent,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			return tx.DB().Exec("SELECT 1").Error
		},
	}}); err != nil {
		t.Fatalf("Consume: %v", err)
	}

	// The bag as Extract leaves it on a request, then the id the caller was answered
	// under — which is what kit/httpx's middleware does, in that order.
	carried, err := baggage.Parse(telemetry.AttrRequestID + "=" + carriedForged +
		"," + telemetry.AttrTenant + "=" + carriedClaimed)
	if err != nil {
		t.Fatalf("the caller's bag does not parse: %v", err)
	}
	publishCtx := telemetry.WithRequestID(baggage.ContextWithBaggage(ctx, carried), answered)

	var stored string
	if err := db.Run(tenancy.WithTenant(publishCtx, acme), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := events.Publish(ctx, tx, carriedEvent, map[string]any{"amount": 1}); err != nil {
				return err
			}
			return tx.DB().Raw("SELECT COALESCE(baggage,'') FROM platformkit_outbox WHERE name = ?",
				carriedEvent).Row().Scan(&stored)
		}); err != nil {
		t.Fatalf("publish inside the carried bag: %v", err)
	}
	if err := events.Relay(t.Context(), conn, tr); err != nil {
		t.Fatalf("relay it: %v", err)
	}

	// The delivery span ends after the handler's transaction, so once it is in the
	// recorder the transaction span is in it too; the transaction is picked as the
	// delivery's child rather than as any transaction span.
	deadline := time.Now().Add(15 * time.Second)
	var delivery, txSpan sdktrace.ReadOnlySpan
	for time.Now().Before(deadline) {
		delivery, txSpan = nil, nil
		for _, s := range recorder.Ended() {
			if s.Name() == carriedEvent+" deliver" {
				delivery = s
			}
		}
		if delivery != nil {
			for _, s := range recorder.Ended() {
				if s.Name() == "database transaction" &&
					s.Parent().SpanID() == delivery.SpanContext().SpanID() {
					txSpan = s
				}
			}
		}
		if txSpan != nil {
			return stored, delivery, txSpan
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no delivery span and handler transaction below it were both recorded (delivery: %v, "+
		"transaction: %v), so this case never reached the spans it is about", delivery, txSpan)
	return "", nil, nil
}

// TestARowWrittenUnderAnUncarriedRequestIDKeepsNoCallerItsOwn: an id Baggage refuses is
// the case where kit/telemetry has nothing to write, and the one where a bag left as it
// arrived would have reached this row and the worker that reads it.
func TestARowWrittenUnderAnUncarriedRequestIDKeepsNoCallerItsOwn(t *testing.T) {
	stored, delivery, txSpan := publishUnderACarriedBag(t, "answered,id,here")

	if strings.Contains(stored, carriedForged) || strings.Contains(stored, carriedClaimed) {
		t.Errorf("the row stores baggage %q, which carries the id and the tenant the caller wrote: the relay "+
			"reads this column into another process, so a worker would name a request nobody was answered for "+
			"and a tenant nobody resolved", stored)
	}
	if stored == "" {
		t.Logf("the row stores no correlation member, which is the NULL migrations/000041 calls the " +
			"ordinary case for a request whose id Baggage will not carry")
	}
	for _, s := range []sdktrace.ReadOnlySpan{delivery, txSpan} {
		if id, has := carriedAttr(s, telemetry.AttrRequestID); has && id.AsString() != "answered,id,here" {
			t.Errorf("%s names %s=%s, which is neither the request the caller was answered for nor nothing",
				s.Name(), telemetry.AttrRequestID, id.AsString())
		}
		deniedBelow(t, s)
	}
}

// TestARowWrittenUnderACarriedRequestIDNamesTheAnsweredOne is the control: the same run
// with an id Baggage carries puts that id on the row and on the delivery span, and
// still no member the caller forged. Without it the case above would pass on a channel
// that had simply stopped carrying anything.
func TestARowWrittenUnderACarriedRequestIDNamesTheAnsweredOne(t *testing.T) {
	stored, delivery, _ := publishUnderACarriedBag(t, "answered-id-here")

	if !strings.Contains(stored, telemetry.AttrRequestID+"=answered-id-here") {
		t.Errorf("the row stores %q, which does not name the request the caller was answered for: an operator "+
			"quoting that id cannot follow it into the worker through the row that was stored to carry it", stored)
	}
	if strings.Contains(stored, carriedForged) || strings.Contains(stored, carriedClaimed) {
		t.Errorf("the row stores %q, which carries a member the caller wrote beside the one the kernel minted",
			stored)
	}
	if id, has := carriedAttr(delivery, telemetry.AttrRequestID); !has || id.AsString() != "answered-id-here" {
		t.Errorf("the delivery span names %s=%v (present: %v), want the id the caller was answered with",
			telemetry.AttrRequestID, id.AsString(), has)
	}
	deniedBelow(t, delivery)
}

// deniedBelow asks one span for the tenant of the row that produced it, and for the
// caller's own two values under any key: the tenant is acme's because the row is
// acme's, and a value a caller wrote in its own header has no way onto this kernel's
// spans at all.
func deniedBelow(t *testing.T, s sdktrace.ReadOnlySpan) {
	t.Helper()
	id, has := carriedAttr(s, telemetry.AttrTenantID)
	if !has {
		t.Errorf("%s carries no %s, so the boundary this event was written in names no tenant",
			s.Name(), telemetry.AttrTenantID)
	} else if id.AsString() != acme.ID.String() {
		t.Errorf("%s names tenant %s, want %s: the tenant of the row that carried the event",
			s.Name(), id.AsString(), acme.ID)
	}
	for _, kv := range s.Attributes() {
		if v := kv.Value.AsString(); v == carriedForged || v == carriedClaimed {
			t.Errorf("%s carries %s=%q — a value the caller wrote into its own Baggage header and no resolver "+
				"ever answered", s.Name(), kv.Key, v)
		}
	}
}

// carriedAttr reads one attribute off a recorded span.
func carriedAttr(s sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}
