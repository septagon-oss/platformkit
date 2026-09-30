package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

// tracesOf is a page of the trail as the trace ids it carries, which is what one of
// these cases reads and the shortest way to say what it found.
func tracesOf(rows []*contracts.Event) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.TraceParent)
	}
	return out
}

// TestATrailRowCarriesTheRequestThatCausedIt is the read-back of
// migrations/000030: the trail answers who, what and when, and now which request.
// The value is the envelope's, the outbox's and the header's — one W3C traceparent,
// stored verbatim, which is the only way the two trail rows one control-plane command
// writes can be read back as one act.
func TestATrailRowCarriesTheRequestThatCausedIt(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()
	tr := trace.New()
	if !tr.Valid() {
		t.Fatal("the fixture's trace context is not a W3C one")
	}
	caused := events.Event{
		ID: uuid.New(), TenantID: acme.ID, Name: "task.task.created", At: db.Now(),
		TraceParent: tr.Parent(), Payload: []byte(`{"taskId":"` + uuid.New().String() + `"}`),
	}
	uncaused := events.Event{
		ID: uuid.New(), TenantID: acme.ID, Name: "task.task.swept", At: db.Now(),
		Payload: []byte(`{}`),
	}

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := svc.Record(ctx, tx, caused); err != nil {
			return err
		}
		return svc.Record(ctx, tx, uncaused)
	})
	if err != nil {
		t.Fatalf("record two events: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		// Get reads one row by its own id; the trail is read as a page, which is
		// how the screen reads it, and the page carries the column with it.
		causedRows, _, err := svc.List(ctx, tx, contracts.Query{Name: caused.Name})
		if err != nil {
			return err
		}
		if len(causedRows) != 1 || causedRows[0].TraceParent != tr.Parent() {
			t.Errorf("the trail of %s carries %v, want the one row holding the request's %q",
				caused.Name, tracesOf(causedRows), tr.Parent())
		}
		quiet, _, err := svc.List(ctx, tx, contracts.Query{Name: uncaused.Name})
		if err != nil {
			return err
		}
		if len(quiet) != 1 || quiet[0].TraceParent != "" {
			t.Errorf("an event nobody caused carries %v", tracesOf(quiet))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the trail back: %v", err)
	}

	// And the column, independently: the empty case is NULL and not the empty
	// string, which is what makes "no request" readable as an absence.
	var stored *string
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Table("audit_events").Select("traceparent").
			Where("event_id = ?", uncaused.ID).Scan(&stored).Error
	}); err != nil {
		t.Fatalf("read the column: %v", err)
	}
	if stored != nil {
		t.Errorf("an event with no request behind it stored %q, want NULL", *stored)
	}
}
