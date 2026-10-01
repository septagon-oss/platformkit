package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

// Which call, from where, on which trace. Three questions the trail could not
// answer, because it is written by a handler in the relay's transaction, after
// the request that caused the event was gone: the facts live only while a call
// is open, so they have to be carried on the event (kit/request → the outbox row
// → the delivery) and kept here.
//
// The case is the whole path the facts take, in the two halves it has: the
// publisher writes them onto its outbox row while the request is live, and the
// trail writes them onto its own row from the delivery. Both are asserted,
// because either half alone leaves a copy missing.
const (
	callID      = "0f7c0f1c-2a3e-4a1b-9b4f-2f1d0c9b8a71"
	callAddress = "203.0.113.7"
)

// callContext is a request: the id the caller was answered with, the peer
// address of the connection, and the trace it opened.
func callContext(t testing.TB, tenant tenancy.Tenant) (context.Context, trace.Context) {
	t.Helper()
	tc := trace.New()
	// Both keys, exactly as kit/httpx's request-id middleware fills them: the
	// trace is kit/trace's own value, and the outbox row reads it from there;
	// kit/request carries the same value, so that one read of a call names the
	// id, the address and the trace together.
	ctx := trace.With(tenancy.WithTenant(t.Context(), tenant), tc)
	return request.With(ctx, request.Context{ID: callID, ClientAddr: callAddress, Trace: tc}), tc
}

// TestTheOutboxRowKeepsWhichCallWroteIt: the kernel's half. An event published
// from a request stores the request; an event published from a job stores
// nothing, and "nothing" is NULL rather than the empty string.
func TestTheOutboxRowKeepsWhichCallWroteIt(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)

	ctx, tc := callContext(t, acme)
	fromJob := uuid.New()
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := events.Publish(ctx, tx, "site.settings_updated", map[string]any{"revision": 3}); err != nil {
			return err
		}
		return events.Publish(tenancy.WithTenant(t.Context(), acme), tx, "task.task.created", map[string]any{"id": fromJob})
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	var id, ip, parent *string
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT request_id, host(client_ip), traceparent FROM platformkit_outbox
			WHERE name = 'site.settings_updated'`).Row().Scan(&id, &ip, &parent)
	})
	if err != nil {
		t.Fatalf("read the outbox row back: %v", err)
	}
	if id == nil || *id != callID {
		t.Errorf("request_id = %v, want the id the caller was answered with", id)
	}
	if ip == nil || *ip != callAddress {
		t.Errorf("client_ip = %v, want the peer address %s", ip, callAddress)
	}
	if parent == nil || *parent != tc.Parent() {
		t.Errorf("traceparent = %v, want the trace the request opened (%s)", parent, tc.Parent())
	}

	var unasked int64
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*) FROM platformkit_outbox
			WHERE name = 'task.task.created' AND request_id IS NULL AND client_ip IS NULL`).
			Scan(&unasked).Error
	})
	if err != nil {
		t.Fatalf("read the job's row back: %v", err)
	}
	if unasked != 1 {
		t.Errorf("an event nobody asked for stored %d request(s), want none stored as NULL", unasked)
	}
}

// TestATrailRowAnswersFromWhere: the module's half, read back through the query
// an operator asks. Who, what, when, from where, and which call.
func TestATrailRowAnswersFromWhere(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()
	tc := trace.New()
	subject, eventID := uuid.New(), uuid.New()

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Record(ctx, tx, events.Event{
			ID: eventID, Name: "site.settings_updated", At: db.Now(),
			Payload:     []byte(`{"id":"` + subject.String() + `","revision":3}`),
			RequestID:   callID,
			ClientIP:    callAddress,
			TraceParent: tc.Parent(),
		})
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	check := func(label string, q contracts.Query) {
		t.Helper()
		var row *contracts.Event
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			rows, total, err := svc.List(ctx, tx, q)
			if err != nil {
				return err
			}
			if total != 1 || len(rows) != 1 {
				return errNoRows
			}
			row = rows[0]
			return nil
		})
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if row.RequestID != callID {
			t.Errorf("%s: requestId = %q, want %q", label, row.RequestID, callID)
		}
		if row.ClientIP != callAddress {
			t.Errorf("%s: clientIp = %q, want %q", label, row.ClientIP, callAddress)
		}
		if row.Traceparent != tc.Parent() {
			t.Errorf("%s: traceparent = %q, want %q", label, row.Traceparent, tc.Parent())
		}
	}
	// By the call, and by the trace id — which is not a column of its own, only
	// the second field of the stored traceparent (migrations/000024) and the
	// expression migrations/000026 indexes.
	check("filtered by request", contracts.Query{Request: callID})
	check("filtered by trace", contracts.Query{TraceID: tc.TraceID})

	// And a read that names neither still returns the row, filtered the way an
	// operator filters it: by the row the event is about, which is the id the
	// payload mentions (migrations/000023), not the id of the event itself.
	var kept int64
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, total, err := svc.List(ctx, tx, contracts.Query{Record: subject})
		kept = total
		return err
	})
	if err != nil || kept != 1 {
		t.Errorf("the trail by record: %d row(s) (%v), want the one row", kept, err)
	}
}

// TestATrailRowWithoutACallStoresAnAbsence: a job's event has no request to
// name, and the trail records that as nothing rather than as the zero of a
// string — the same absence migrations/000024 says an absence is.
func TestATrailRowWithoutACallStoresAnAbsence(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Record(ctx, tx, events.Event{
			ID: uuid.New(), Name: "task.task.created", At: db.Now(), Payload: []byte(`{}`),
		})
	})
	if err != nil {
		t.Fatalf("record a job's event: %v", err)
	}
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, total, err := svc.List(ctx, tx, contracts.Query{})
		if err != nil {
			return err
		}
		if total != 1 || len(rows) != 1 {
			return errNoRows
		}
		if rows[0].RequestID != "" || rows[0].ClientIP != "" || rows[0].Traceparent != "" {
			t.Errorf("an event with no request stored %q/%q/%q",
				rows[0].RequestID, rows[0].ClientIP, rows[0].Traceparent)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	// The other tenant's List stays empty, which is the boundary the three new
	// columns did not move: RLS answers before any filter is applied.
	err = db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, total, err := svc.List(ctx, tx, contracts.Query{Request: callID}); err != nil || total != 0 {
			t.Errorf("globex found %d of acme's rows by request (%v)", total, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("list as globex: %v", err)
	}
}

var errNoRows = errors.New("the page was empty")
