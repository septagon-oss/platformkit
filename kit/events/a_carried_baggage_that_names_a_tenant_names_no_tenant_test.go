package events_test

// Review round 12 (T-0110). The merged tree gave the outbox row a third propagated
// member and gave the CloudEvents envelope a third extension attribute: `baggage`,
// written by `traceContext` through the global propagator (kit/events/trace.go) and
// read back by `startDelivery`, which extracts it onto the handler's context.
//
// Neither side of that channel ever says what a caller may put *into* the bag.
// kit/telemetry writes exactly one member of its own, `pkit.request.id`, and it
// writes the bag whole (`baggage.New(m)` replaces the context's bag rather than
// adding to it), so a request through kit/httpx normally arrives with a bag that
// holds only what the router minted. But the member the delivery reads is the whole
// `Baggage` header, and both writers of the row inject whatever the bag holds:
//
//	write        → "…(traceparent), (tracestate), baggage"      (kit/events/events.go)
//	startDelivery→ Extract{traceparent, tracestate, baggage}    (kit/events/trace.go)
//
// So the question this round asks is the one the two channels make new: can a value
// a caller chose, carried in the bag its own request left on the row, name the
// *tenant* on the spans the delivery and the handler's transaction leave behind? It
// could plausibly come to pass — `kit/events/trace.go` debates, in the last paragraph
// of its comment, that the delivery's tenant is an id and not a slug and that getting
// the slug "would be a query per delivery, which is not a price tracing should
// charge". A bag member holding the slug is the tempting way round that query, and
// the day somebody takes it, one tenant's request would name another tenant on the
// kernel's spans and numbers.
//
// The case therefore carries, inside the publisher's bag, the two keys the kernel
// itself uses (`pkit.tenant`, `pkit.tenant.id`) with the values of a *different*
// tenant, and asserts what every span below the delivery names: the tenant of the
// row the event was written into, and never the tenant the bag claims. It reaches
// every assertion through what works today — the handler ran, its transaction span is
// on the publisher's trace and names the event's own tenant, and the delivery span
// names the request id the bag's kernel member carried — so the case does not need
// the defect's own output to get to the point where it can deny it.

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

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// round12Claimed is the tenant the caller's own bag claims: a second customer of this
// schema, whose rows the publisher may not see and whose name none of this tenant's
// spans may carry.
var round12Claimed = tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}

const round12Event = "billing.invoice_issued"

// TestACarriedBaggageThatNamesATenantNamesNoTenantOnTheDelivery is the claim: the
// tenant on the delivery span and on the handler's transaction span is the tenant the
// event's row belongs to, whatever the propagated correlation set carries.
func TestACarriedBaggageThatNamesATenantNamesNoTenantOnTheDelivery(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	otel.SetTextMapPropagator(telemetry.Propagators())

	const requestID = "9a8b7c6d-1111-4222-9333-445566778899"
	_, conn := dbtest.Schema(t)

	ran := make(chan struct{}, 1)
	tr := memory.New()
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	if err := events.Consume(ctx, conn, tr, []events.Subscription{{
		Module: "billing", Name: round12Event,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			// The handler's own transaction: the span below the delivery span,
			// opened by kit/db on the handler's context (see kit/db/tx.go).
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

	// The publisher's bag: the member the kernel minted, and two members the caller
	// minted that name another tenant by both keys the kernel writes.
	minted, err := baggage.NewMember(telemetry.AttrRequestID, requestID)
	if err != nil {
		t.Fatalf("the kernel's own request id is not a baggage member: %v", err)
	}
	claimedSlug, err := baggage.NewMember(telemetry.AttrTenant, round12Claimed.Slug)
	if err != nil {
		t.Fatalf("a caller's slug is not a baggage member: %v", err)
	}
	claimedID, err := baggage.NewMember(telemetry.AttrTenantID, round12Claimed.ID.String())
	if err != nil {
		t.Fatalf("a caller's tenant id is not a baggage member: %v", err)
	}
	bag, err := baggage.New(minted, claimedSlug, claimedID)
	if err != nil {
		t.Fatalf("the caller's bag does not build: %v", err)
	}
	// The tenant of the transaction is acme — the tenant whose rows the INSERT runs
	// under — which is the fact the spans must report and the bag must not overwrite.
	publishCtx := baggage.ContextWithBaggage(t.Context(), bag)
	if err := db.Run(tenancy.WithTenant(publishCtx, acme), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, round12Event, map[string]any{"amount": 1})
		}); err != nil {
		t.Fatalf("publish inside the caller's bag: %v", err)
	}
	if err := events.Relay(t.Context(), conn, tr); err != nil {
		t.Fatalf("relay it: %v", err)
	}

	// Reachability, from three facts that hold today and none of which is the point:
	// the handler ran, the delivery span exists, and the handler's transaction is on
	// the publisher's trace below it.
	// The delivery span ends after the handler's transaction, so once it is in the
	// recorder the transaction span is in it too; both are read together, and the
	// transaction is picked by being the delivery span's child rather than by being
	// any transaction span, or the publisher's and the relay's own would do as well.
	deadline := time.Now().Add(15 * time.Second)
	var delivery, txSpan sdktrace.ReadOnlySpan
	for time.Now().Before(deadline) {
		delivery, txSpan = nil, nil
		for _, s := range recorder.Ended() {
			if s.Name() == round12Event+" deliver" {
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
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if delivery == nil {
		t.Fatal("no delivery span was recorded, so the handler was never reached and this case " +
			"never got to the assertion it exists to make")
	}
	if txSpan == nil {
		t.Fatalf("no %q span child of the delivery span was recorded (spans: %v), so the handler's "+
			"transaction — the span below the one the delivery context is read from — was never reached",
			"database transaction", round12Names(recorder.Ended()))
	}
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("the handler's span was recorded but the handler never reported running")
	}

	// Every span below the delivery names the tenant of the row, both of them.
	for _, s := range []sdktrace.ReadOnlySpan{delivery, txSpan} {
		got, has := round12Attr(s, telemetry.AttrTenantID)
		if !has {
			t.Errorf("%s carries no %s, so the delivery's tenant boundary is not being written at all",
				s.Name(), telemetry.AttrTenantID)
			continue
		}
		if got.AsString() != acme.ID.String() {
			t.Errorf("%s names tenant %s, want %s (the tenant the event's row belongs to)",
				s.Name(), got.AsString(), acme.ID.String())
		}
		// The bag's tenant-shaped members must not appear under any key, under either
		// spelling: a slug on a delivery span would be the invented slug
		// kit/telemetry/README.md promises a span never carries, and an id would be
		// one tenant charged with another tenant's work.
		for _, kv := range s.Attributes() {
			if v := kv.Value.AsString(); v == round12Claimed.Slug || v == round12Claimed.ID.String() {
				t.Errorf("%s carries %s=%q: a tenant the caller named in its own Baggage header "+
					"reached the kernel's span through the propagated correlation set the delivery "+
					"extracts (kit/events/trace.go, startDelivery)", s.Name(), kv.Key, v)
			}
		}
	}

	// And the kernel's own member still arrives: the request id the caller saw in
	// X-Request-ID is on both spans, which is what the baggage column is for.
	if id, has := round12Attr(delivery, telemetry.AttrRequestID); !has || id.AsString() != requestID {
		t.Errorf("the delivery span names request %q (present: %v), want %q: the case cannot tell "+
			"which request it speaks of if the kernel's own member does not arrive",
			id.AsString(), has, requestID)
	}
}

// TestTheRowKeepsTheBagItWasGivenAndNothingElse is the same channel from the storage
// side: the three members are the row's, and the caller's member is stored because it
// is the third member of the propagated set — so this case records what the channel
// *does* carry, and pins the two facts the delivery span assertions above rest on:
// the baggage member arrives on the row, and the row's tenant is the tenant that
// published, not the one the bag named.
func TestTheRowKeepsTheBagItWasGivenAndNothingElse(t *testing.T) {
	otel.SetTextMapPropagator(telemetry.Propagators())
	_, conn := dbtest.Schema(t)

	const requestID = "11223344-aaaa-4bbb-8ccc-ddddaabbccdd"
	minted, err := baggage.NewMember(telemetry.AttrRequestID, requestID)
	if err != nil {
		t.Fatalf("the kernel's own request id is not a baggage member: %v", err)
	}
	claimed, err := baggage.NewMember(telemetry.AttrTenant, round12Claimed.Slug)
	if err != nil {
		t.Fatalf("a caller's slug is not a baggage member: %v", err)
	}
	bag, err := baggage.New(minted, claimed)
	if err != nil {
		t.Fatalf("the caller's bag does not build: %v", err)
	}

	var rowTenant uuid.UUID
	var parent, state, correlation string
	err = db.Run(tenancy.WithTenant(baggage.ContextWithBaggage(t.Context(), bag), acme), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := events.Publish(ctx, tx, round12Event, map[string]any{"amount": 2}); err != nil {
				return err
			}
			return tx.DB().Raw("SELECT tenant_id, COALESCE(traceparent,''), COALESCE(tracestate,''), "+
				"COALESCE(baggage,'') FROM platformkit_outbox WHERE name = ?", round12Event).
				Row().Scan(&rowTenant, &parent, &state, &correlation)
		})
	if err != nil {
		t.Fatalf("publish and read the row back: %v", err)
	}

	// The row is the publisher tenant's row, so the row's own columns are the honest
	// reachability probe: a row that exists and belongs to acme.
	if rowTenant != acme.ID {
		t.Fatalf("the outbox row's tenant_id = %s, want %s: the case reads a row that is not the "+
			"publisher's, so nothing below means anything", rowTenant, acme.ID)
	}
	if correlation == "" {
		t.Fatal("the row carries no baggage member at all, so the correlation channel this case is " +
			"about does not exist and the delivery span cannot name its request")
	}
	// The stored member names the request the router minted. That the caller's own
	// member travels beside it is the fact the case above denies on the spans; here it
	// is what the row does, recorded rather than denied, so a change to either half of
	// the channel shows up in one of the two cases and not in neither.
	if !strings.Contains(correlation, telemetry.AttrRequestID+"="+requestID) {
		t.Errorf("the row's baggage member is %q, which does not name the request %q the router "+
			"minted: the request id does not reach the delivery through the row it was stored for",
			correlation, requestID)
	}
	if parent != "" || state != "" {
		t.Errorf("a publish with no span open stored traceparent=%q tracestate=%q, want both empty: "+
			"an untraced publish must leave NULLs, which is the fact kit/events/trace.go stores",
			parent, state)
	}
}

// round12Attr reads one attribute off a recorded span.
func round12Attr(s sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

func round12Names(all []sdktrace.ReadOnlySpan) []string {
	out := make([]string, 0, len(all))
	for _, s := range all {
		out = append(out, s.Name())
	}
	return out
}
