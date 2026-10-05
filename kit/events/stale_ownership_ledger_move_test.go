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

func TestAClaimWaitingAtTheTableCannotRunAgainAfterTheLedgerMove(t *testing.T) {
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

	// A SHARE table lock, as taken by CREATE INDEX, lets readers and an empty
	// ledger move proceed while an INSERT waits before its row trigger runs.
	// The delivery has passed its ownership reads but acquired no claim lock.
	blocker, err := owner.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err := blocker.ExecContext(ctx,
		`LOCK TABLE platformkit_handled IN SHARE MODE`); err != nil {
		t.Fatal(err)
	}
	var blockerPID int
	if err := blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	effect := "ledger_" + tenant.ID.String()[:8] + ".effect_committed"
	handle := func(ctx context.Context, tx db.Tx[db.Tenant], _ Event) error {
		if err := Publish(ctx, tx, effect, nil); err != nil {
			return err
		}
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
	var deliveryPID int
	for {
		if err := owner.QueryRowContext(ctx, `SELECT coalesce(max(pid), 0)
			FROM pg_stat_activity
			WHERE application_name = current_setting('search_path')
			AND pg_blocking_pids(pid) @> ARRAY[$1::integer]
			AND query LIKE 'INSERT INTO platformkit_handled%'`, blockerPID).Scan(&deliveryPID); err != nil {
			t.Fatal(err)
		}
		if deliveryPID != 0 {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("the old delivery did not reach its claim: ", ctx.Err())
		}
	}
	// A corrected ownership check may hold the tenant row until handling commits.
	// Allow that safe order as well as placement finishing before the claim.
	placer, err := owner.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer placer.Close()
	var placerPID int
	if err := placer.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&placerPID); err != nil {
		t.Fatal(err)
	}
	placed := make(chan error, 1)
	go func() {
		_, err := placer.ExecContext(ctx, `SELECT platformkit_place_tenants('collect', '', 'shop=collect', NULL)`)
		placed <- err
	}()
	var placementErr error
placing:
	for {
		select {
		case placementErr = <-placed:
			break placing
		default:
		}
		var waiting bool
		if err := owner.QueryRowContext(ctx,
			`SELECT pg_blocking_pids($1::integer) @> ARRAY[$2::integer]`, placerPID, deliveryPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			if err := blocker.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case placementErr = <-placed:
			case <-ctx.Done():
				t.Fatal("placement did not finish after the claim was released: ", ctx.Err())
			}
			break placing
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("placement neither completed nor waited on the claim: ", ctx.Err())
		}
	}
	if placementErr != nil {
		t.Fatal(placementErr)
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
	// Rollback also succeeds harmlessly if the safe placement branch already
	// committed the read-only blocker above.
	_ = blocker.Rollback()
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
		t.Errorf("one event was handled %d times across the move after its legacy claim waited before the row trigger; want exactly once", got)
	}
	var effects int
	if err := owner.QueryRowContext(ctx,
		`SELECT count(*) FROM platformkit_outbox WHERE tenant_id=$1 AND name=$2`, tenant.ID, effect).Scan(&effects); err != nil {
		t.Fatal(err)
	}
	if effects != 1 {
		t.Errorf("the event committed %d transactional effects across the move, want exactly one", effects)
	}
}
