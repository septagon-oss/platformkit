package internal_test

// The core review of 2026-09-29's three audit cases (P1 "Audit history is mutable by
// the ordinary application database role" and P2 "The audit trail cannot account for
// every published event"), ported into the module from
// refs/reviews/pkit-core-2026-09-29/audit_review_test.go and green. They are kept
// recognisable — same names, same assertions, same reported failures — and the file
// says in a comment wherever the delivery changed the world the case was written
// against. Case 2 of the three is a settings save and so lives with the settings, in
// modules/site/internal/a_settings_save_publishes_the_tagline_it_replaced_test.go.
//
// They run as the real application role through dbtest.Schema, in disposable schemas,
// the way reproductions.log ran them.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

// TestReviewAuditStorageRejectsMutation is the review's P1 case, unchanged: an event
// recorded through the real audit service, then an UPDATE and a DELETE through
// ordinary tenant transactions. Both used to commit (reproductions.log: "ordinary
// tenant transaction committed an UPDATE of audit history"); both are refused now, by
// the revoke where the deployment pinned the privilege and by migrations/000048's
// triggers wherever it did not.
func TestReviewAuditStorageRejectsMutation(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	ctx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New()})
	svc := internal.NewService()
	ev := events.Event{ID: uuid.New(), Name: "task.task.created", At: db.Now(), Payload: []byte(`{}`)}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Record(ctx, tx, ev)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("UPDATE audit_events SET name = 'task.forged' WHERE event_id = ?", ev.ID).Error
	}); err == nil {
		t.Error("ordinary tenant transaction committed an UPDATE of audit history")
	}
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("DELETE FROM audit_events WHERE event_id = ?", ev.ID).Error
	}); err == nil {
		t.Error("ordinary tenant transaction committed a DELETE of audit history")
	}
	// The trail the review's two statements went through is unchanged: the refusal
	// arrives before the write, not instead of it.
	var name string
	var rows int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*), max(name) FROM audit_events`).Scan(&rows, &name); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || name != "task.task.created" {
		t.Errorf("the trail holds %d rows named %q, want the one event as it was recorded", rows, name)
	}
}

// TestReviewPublishedEventsAllReachAudit is the review's P2 case. The delivery changed
// what this case may assert, and says so: it used to publish a name no manifest
// declared, watch it be accepted and stamped, and fail on the count mismatch
// ("accepted and stamped 2 events, recorded 1 audit events"). An undeclared name is
// now refused at the write door (kit/events/catalog.go checkDeclared), so the case
// asserts the refusal and then the arithmetic the review wanted — every event the
// outbox stamped published has its trail row.
func TestReviewPublishedEventsAllReachAudit(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	ctx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New()})
	mods := module.Expand([]module.Module{
		{Name: "orders", Declared: []events.Declared{events.Declare[orderPlaced]("orders.placed")}},
		audit.New(audit.Deps{}),
	})
	if err := module.Validate(mods); err != nil {
		t.Fatal(err)
	}
	// The catalog is what kit/app installs from the composition; a test that names
	// the modules has to install it too, or it is testing a process with no modules.
	events.DeclareAll(mods[0].Emits())
	t.Cleanup(func() { events.DeclareAll(nil) })
	transport := memory.New()
	if err := events.Consume(t.Context(), conn, transport, mods[1].Subscriptions); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, "orders.placed", orderPlaced{OrderID: uuid.New()})
	}); err != nil {
		t.Fatalf("publish the declared event: %v", err)
	}
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatal(err)
	}
	// The same call with a name no manifest declares, which used to be accepted.
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, "orders.undeclared", map[string]string{"id": uuid.NewString()})
	}); err == nil {
		t.Error("a publication of a name no module declares was accepted, so no subscriber can ever receive it")
	}
	var published, audited int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE published_at IS NOT NULL").Scan(&published); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM audit_events").Scan(&audited); err != nil {
		t.Fatal(err)
	}
	if published == 0 {
		t.Fatal("the trail was compared against an outbox that holds nothing, which proves nothing")
	}
	if published != audited {
		t.Errorf("accepted and stamped %d events, recorded %d audit events; a publication was silently lost to audit", published, audited)
	}
}

// orderPlaced is the synthetic module's payload.
type orderPlaced struct {
	OrderID uuid.UUID `json:"orderId"`
}
