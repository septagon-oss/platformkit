package events

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	provider "github.com/septagon-oss/platformkit/kit/events/providers/nats"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAClaimQueuedBeforePlacementCannotRunAgainAfterTheLedgerMove(t *testing.T) {
	owner, conn := dbtest.Schema(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	old, js := jetstreamForTest(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "shop"}
	if _, err := owner.ExecContext(ctx,
		`INSERT INTO tenants (id, slug, name, app) VALUES ($1, 'shop', 'Shop', '')`, tenant.ID); err != nil {
		t.Fatal(err)
	}
	name := "ledger_" + tenant.ID.String()[:8] + ".invoice_issued"
	unscoped := appname.Durable("", "ledger", name)
	scoped := appname.Durable("collect", "ledger", name)
	t.Cleanup(func() {
		_ = old.(interface{ Close() error }).Close()
		for _, durable := range []string{unscoped, scoped} {
			_ = js.DeleteConsumer(stream, durable)
		}
		for _, app := range []appname.Name{"", "collect"} {
			_ = js.PurgeStream(stream, &nats.StreamPurgeRequest{Subject: appname.Subject(app, tenant.ID, name)})
		}
	})

	// An exclusive durable lock models another move's transaction. A legacy
	// delivery can already have read app='' while its claim waits on this key,
	// before the trigger reaches the shared lock of its own tenant.
	blocker, err := owner.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err := blocker.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, durableLock(unscoped)); err != nil {
		t.Fatal(err)
	}
	var blockerPID int
	if err := blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	handle := func(context.Context, db.Tx[db.Tenant], Event) error {
		calls.Add(1)
		return nil
	}
	if err := Consume(ctx, conn, old, []Subscription{{Module: "ledger", Name: name, Handler: handle}}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(tenancy.WithTenant(ctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return Publish(ctx, tx, name, nil)
	}); err != nil {
		t.Fatal(err)
	}
	// The old publisher finishes and closes before naming the app. Only its
	// previously accepted delivery remains in flight, so no old-address
	// publisher survives the documented rollout boundary.
	publisher, err := provider.JetStream("", os.Getenv("PLATFORMKIT_TEST_NATS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = publisher.(interface{ Close() error }).Close() })
	if err := Relay(ctx, conn, publisher); err != nil {
		t.Fatal(err)
	}
	if err := publisher.(interface{ Close() error }).Close(); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var queued bool
		if err := owner.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE application_name = current_setting('search_path')
			AND pg_blocking_pids(pid) @> ARRAY[$1::integer]
			AND query LIKE 'INSERT INTO platformkit_handled%')`, blockerPID).Scan(&queued); err != nil {
			t.Fatal(err)
		}
		if queued {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("the old delivery did not reach its claim: ", ctx.Err())
		}
	}
	if err := dbtest.System(ctx, conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec(`SELECT platformkit_place_tenants('collect', '', 'shop=collect', NULL)`).Error
	}); err != nil {
		t.Fatal(err)
	}
	report, moveErr := MoveLedger(ctx, conn, "collect", "boot")
	if moveErr != nil && !errors.Is(moveErr, ErrLedgerMoveContended) {
		t.Fatal(moveErr)
	}
	if moveErr != nil {
		var recorded int
		if err := owner.QueryRowContext(ctx,
			`SELECT count(*) FROM platformkit_outbox WHERE name=$1`, EventLedgerMoved).Scan(&recorded); err != nil {
			t.Fatal(err)
		}
		if report != (MoveReport{}) || recorded != 0 {
			t.Errorf("refused move returned %+v and emitted %d records, want neither", report, recorded)
		}
	}
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	waitForAck := func(durable string) {
		t.Helper()
		for {
			info, err := js.ConsumerInfo(stream, durable)
			if err != nil {
				t.Fatal(err)
			}
			if info.AckFloor.Consumer >= 1 && info.NumPending == 0 && info.NumAckPending == 0 {
				return
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				t.Fatalf("consumer %s did not finish its delivery: %+v", durable, info)
			}
		}
	}
	waitForAck(unscoped)
	if moveErr != nil {
		if _, err := MoveLedger(ctx, conn, "collect", "boot"); err != nil {
			t.Fatal(err)
		}
	}
	var id uuid.UUID
	if err := owner.QueryRowContext(ctx,
		`SELECT id FROM platformkit_outbox WHERE name=$1 AND tenant_id=$2`, name, tenant.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ExecContext(ctx, `UPDATE platformkit_outbox SET published_at=NULL WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	fresh, err := provider.JetStream("collect", os.Getenv("PLATFORMKIT_TEST_NATS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.(interface{ Close() error }).Close() })
	if err := RelayApp(ctx, conn, fresh, "collect"); err != nil {
		t.Fatal(err)
	}
	if err := Consume(ctx, conn, fresh, []Subscription{{App: "collect", Module: "ledger", Name: name, Handler: handle}}); err != nil {
		t.Fatal(err)
	}
	waitForAck(scoped)
	if got := calls.Load(); got != 1 {
		t.Errorf("one event was handled %d times across the move after its legacy claim queued before placement; want exactly once", got)
	}
}
