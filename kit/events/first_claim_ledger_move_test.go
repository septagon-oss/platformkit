package events_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestALedgerMoveWaitsForTheFirstClaimBeforeOpeningScopedConsumers(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "shop"}
	placeTenant(t, conn, tenant.ID, tenant.Slug, "")
	// A distinct event also gives this durable its own advisory lock, so traffic
	// in another schema cannot make an empty-ledger move appear to refuse safely.
	name := "ledger_" + tenant.ID.String()[:8] + ".invoice_issued"
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	old := memory.New()
	entered, release := make(chan struct{}), make(chan struct{})
	var calls counter
	if err := events.Consume(ctx, conn, old, []events.Subscription{{
		Module: ledgerModule, Name: name,
		Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error {
			close(entered)
			select {
			case <-release:
				return calls.run()
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}}); err != nil {
		t.Fatal(err)
	}
	publish(t, conn, tenant, name, nil)
	id := eventID(t, conn, tenant.ID, name)
	finished := make(chan error, 1)
	go func() { finished <- events.Relay(ctx, conn, old) }()
	select {
	case <-entered:
	case err := <-finished:
		t.Fatalf("old consumer never opened its claim: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// The late named boot places this tenant while its old consumer still holds
	// the first uncommitted handling claim.
	if err := dbtest.System(ctx, conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec("UPDATE tenants SET app = 'collect' WHERE id = ?", tenant.ID).Error
	}); err != nil {
		t.Fatal(err)
	}
	report, moveErr := events.MoveLedger(ctx, conn, "collect", "boot")
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(moveErr, events.ErrLedgerMoveContended) {
		t.Errorf("move while the first claim is open: report=%+v error=%v; want contention before consumers open", report, moveErr)
	}
	if report != (events.MoveReport{}) {
		t.Errorf("refused move returned a partial report: %+v", report)
	}
	if records := movedRecords(t, conn, tenant.ID); len(records) != 0 {
		t.Errorf("mid-claim move emitted %d records, want none", len(records))
	}
	if moveErr != nil {
		// A safe boot retries after the old transaction commits, then subscribes.
		if _, err := events.MoveLedger(ctx, conn, "collect", "boot"); err != nil {
			t.Fatal(err)
		}
	}
	scoped := memory.New()
	if err := events.Consume(ctx, conn, scoped, []events.Subscription{{
		App: "collect", Module: ledgerModule, Name: name,
		Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error { return calls.run() },
	}}); err != nil {
		t.Fatal(err)
	}
	unstamp(t, conn, id)
	if err := events.RelayApp(ctx, conn, scoped, "collect"); err != nil {
		t.Fatal(err)
	}
	if got := calls.count(); got != 1 {
		t.Errorf("the same event was handled %d times across the durable move, want once", got)
	}
}
