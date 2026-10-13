package events_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestPlacementBeforeLedgerCommitCannotRepeatCommittedWork(t *testing.T) {
	owner, conn := dbtest.Schema(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	early := tenantID(t, conn, "early", "collect")
	claimRow(t, conn, appname.Durable("", ledgerModule, "ledger_"+early.String()[:8]+".invoice_issued"), uuid.New(), early)
	late := tenancy.Tenant{ID: uuid.New(), Slug: "late"}
	placeTenant(t, conn, late.ID, late.Slug, "")
	name := "ledger_" + late.ID.String()[:8] + ".invoice_issued"
	effect := "ledger_" + late.ID.String()[:8] + ".effect_committed"
	publish(t, conn, late, name, nil)
	id := eventID(t, conn, late.ID, name)
	var calls atomic.Int64
	handle := func(ctx context.Context, tx db.Tx[db.Tenant], _ events.Event) error {
		calls.Add(1)
		return events.Publish(ctx, tx, effect, nil)
	}
	old := memory.New()
	if err := events.Consume(ctx, conn, old, []events.Subscription{{Module: ledgerModule, Name: name, Handler: handle}}); err != nil {
		t.Fatal(err)
	}
	if err := events.Relay(ctx, conn, old); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("the initial delivery did not handle its event exactly once")
	}
	before := ledgerShape(t, conn)

	// A schema operation holds the outbox write after the move's final ledger
	// read. The tenant placement writes another table and can commit meanwhile.
	blocker, err := owner.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err := blocker.ExecContext(ctx, `LOCK TABLE platformkit_outbox IN SHARE MODE`); err != nil {
		t.Fatal(err)
	}
	var blockerPID int
	if err := blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
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
	tick := time.Tick(10 * time.Millisecond)
waiting:
	for {
		select {
		case result = <-moved:
			moveDone = true
			break waiting
		default:
		}
		var waiting bool
		if err := owner.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE application_name = current_setting('search_path')
			AND pg_blocking_pids(pid) @> ARRAY[$1::integer]
			AND query LIKE 'INSERT INTO platformkit_outbox%')`, blockerPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-tick:
		case <-ctx.Done():
			t.Fatal("move neither answered nor reached its outbox write: ", ctx.Err())
		}
	}
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
		_, err := placer.ExecContext(ctx, `SELECT platformkit_place_tenants('collect', '', 'late=collect', NULL)`)
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
	if result.err != nil {
		if !errors.Is(result.err, events.ErrLedgerMoveContended) || result.report != (events.MoveReport{}) {
			t.Fatalf("refusal returned %+v, want contention with an empty report", result)
		}
		if ledgerShape(t, conn) != before || len(movedRecords(t, conn, early))+len(movedRecords(t, conn, late.ID)) != 0 {
			t.Fatal("refused move changed ledger rows or emitted a record")
		}
	}
	if !placementDone {
		if err := <-placed; err != nil {
			t.Fatal(err)
		}
	}
	// A serialized placement happens after this move and owes its own move.
	// A refused move must likewise be retried before opening scoped consumers.
	if !placementDone || moveDone || result.err != nil {
		result.report, result.err = events.MoveLedger(ctx, conn, "collect", "boot")
		if result.err != nil {
			t.Fatal(result.err)
		}
	}
	t.Logf("placement committed before move completed=%v; report=%+v error=%v", placementDone && !moveDone, result.report, result.err)
	scoped := memory.New()
	if err := events.Consume(ctx, conn, scoped, []events.Subscription{{App: "collect", Module: ledgerModule, Name: name, Handler: handle}}); err != nil {
		t.Fatal(err)
	}
	unstamp(t, conn, id)
	if err := events.RelayApp(ctx, conn, scoped, "collect"); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("one event was handled %d times after placement before ledger commit; want once", got)
	}
	var effects int
	if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_outbox WHERE tenant_id=$1 AND name=$2`, late.ID, effect).Scan(&effects); err != nil {
		t.Fatal(err)
	}
	if effects != 1 {
		t.Errorf("one event committed %d effects after placement before ledger commit; want one", effects)
	}
}
