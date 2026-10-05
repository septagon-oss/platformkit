package events_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestPlacementDuringALedgerMoveCannotLeaveAClaimBehind(t *testing.T) {
	owner, conn := dbtest.Schema(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "shop"}
	placeTenant(t, conn, tenant.ID, tenant.Slug, "")
	name := "ledger_" + tenant.ID.String()[:8] + ".invoice_issued"
	effect := "ledger_" + tenant.ID.String()[:8] + ".effect_committed"
	publish(t, conn, tenant, name, nil)
	id := eventID(t, conn, tenant.ID, name)

	// A schema operation can hold the ledger relation while the move has already
	// read its tenant set. It also holds a legacy claim before its row trigger.
	blocker, err := owner.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err := blocker.ExecContext(ctx, `LOCK TABLE platformkit_handled IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	var blockerPID int
	if err := blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	tick := time.Tick(10 * time.Millisecond)
	waitForStatement := func(pattern string) {
		t.Helper()
		for {
			var waiting bool
			if err := owner.QueryRowContext(ctx, `SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE application_name = current_setting('search_path')
				AND pg_blocking_pids(pid) @> ARRAY[$1::integer] AND query LIKE $2)`,
				blockerPID, pattern).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting {
				return
			}
			select {
			case <-tick:
			case <-ctx.Done():
				t.Fatalf("statement %q never reached the held ledger: %v", pattern, ctx.Err())
			}
		}
	}
	var calls atomic.Int64
	handle := func(ctx context.Context, tx db.Tx[db.Tenant], _ events.Event) error {
		calls.Add(1)
		return events.Publish(ctx, tx, effect, nil)
	}
	old := memory.New()
	if err := events.Consume(ctx, conn, old, []events.Subscription{{Module: ledgerModule, Name: name, Handler: handle}}); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- events.Relay(ctx, conn, old) }()
	waitForStatement("INSERT INTO platformkit_handled%")

	type moveResult struct {
		report events.MoveReport
		err    error
	}
	moved := make(chan moveResult, 1)
	go func() {
		r, err := events.MoveLedger(ctx, conn, "collect", "boot")
		moved <- moveResult{r, err}
	}()

	var result moveResult
	moveDone := false
moving:
	for {
		select {
		case result = <-moved:
			moveDone = true
			break moving
		default:
		}
		var waiting bool
		if err := owner.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE application_name = current_setting('search_path')
			AND pg_blocking_pids(pid) @> ARRAY[$1::integer]
			AND query LIKE 'SELECT DISTINCT durable FROM platformkit_handled%')`, blockerPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-tick:
		case <-ctx.Done():
			t.Fatal("move neither answered nor reached its ledger read: ", ctx.Err())
		}
	}

	// Placement can originate from another declaring boot while this move is
	// suspended. Allow a cure to serialize placement behind the move or claim.
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
	placementDone := false
placing:
	for {
		select {
		case err := <-placed:
			if err != nil {
				t.Fatal(err)
			}
			placementDone = true
			break placing
		default:
		}
		var waiting bool
		if err := owner.QueryRowContext(ctx, `SELECT cardinality(pg_blocking_pids($1::integer)) > 0`, placerPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-tick:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	if !moveDone {
		result = <-moved
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if !placementDone {
		if err := <-placed; err != nil {
			t.Fatal(err)
		}
		// A placement serialized after the completed move needs its own move.
		result.report, result.err = events.MoveLedger(ctx, conn, "collect", "boot")
	}
	if result.err != nil {
		if !errors.Is(result.err, events.ErrLedgerMoveContended) {
			t.Fatal(result.err)
		}
		if result.report != (events.MoveReport{}) || len(movedRecords(t, conn, tenant.ID)) != 0 {
			t.Fatalf("refused move returned %+v or committed a record", result.report)
		}
		if _, err := events.MoveLedger(ctx, conn, "collect", "boot"); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("placement completed during move=%v; move report=%+v error=%v", placementDone, result.report, result.err)
	scoped := memory.New()
	if err := events.Consume(ctx, conn, scoped, []events.Subscription{{App: "collect", Module: ledgerModule, Name: name, Handler: handle}}); err != nil {
		t.Fatal(err)
	}
	unstamp(t, conn, id)
	if err := events.RelayApp(ctx, conn, scoped, "collect"); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("one event was handled %d times after placement during the ledger move; want once", got)
	}
	var effects int
	if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_outbox WHERE tenant_id=$1 AND name=$2`, tenant.ID, effect).Scan(&effects); err != nil {
		t.Fatal(err)
	}
	if effects != 1 {
		t.Errorf("one event committed %d effects after placement during the ledger move; want one", effects)
	}
}
