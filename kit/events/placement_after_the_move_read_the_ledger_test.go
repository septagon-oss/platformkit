package events_test

// placement_after_the_move_read_the_ledger_test.go: a tenant whose app arrives while the
// move is in flight costs neither tenant its claim.
//
// The rename copies each ledger onto the app's prefix and then deletes what it copied. The
// tenant boundary both halves read — `tenant_id IN (SELECT id FROM tenants WHERE app = ?)`
// — answers a question about another table, and the placement answers it differently the
// moment it commits. So the delete names the keys the copy read, one reading for both
// halves, and the move ends by asking whether anything it had to rename is still unscoped.
// A reading and a writing that agree is the difference between a move that renames the late
// tenant's claim and one that leaves it behind or removes it: the mark that stops a handler
// running twice is not something the move that exists to stop handlers running twice may
// drop on the way past.
//
// The case reaches the move with a table lock, the kind a schema operation takes: EXCLUSIVE
// mode stands in front of the move's writes (ROW SHARE, ROW EXCLUSIVE) and lets its reading
// (ACCESS SHARE) walk past, and the placement commits through the tenants table, which
// nothing here holds. Which safe answer the move gives depends on which side of its own
// reading the placement landed, and both are allowed below: the refusal that writes and
// emits nothing, or the rename that took the late tenant with it. What neither may do is
// report one claim renamed and leave the other nowhere.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
)

func TestAPlacementAfterTheMoveReadsTheLedgerCannotCostATenantItsClaim(t *testing.T) {
	owner, conn := dbtest.Schema(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	durable := appname.Durable(appname.Name(""), ledgerModule, ledgerEvent)
	scoped := appname.Durable("collect", ledgerModule, ledgerEvent)
	early := tenantID(t, conn, "collect-early", "collect")
	late := uuid.New()
	placeTenant(t, conn, late, "late", "")
	earlyEvent, lateEvent := uuid.New(), uuid.New()
	claimRow(t, conn, durable, earlyEvent, early)
	claimRow(t, conn, durable, lateEvent, late)
	// Both claims under one durable, so the move names it from the tenant it already
	// holds and the late tenant's row falls inside the set the delete reaches for.
	before := ledgerShape(t, conn)

	blocker, err := owner.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err := blocker.ExecContext(ctx, `LOCK TABLE platformkit_handled IN EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	var blockerPID int
	if err := blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	waitFor := func(query, what string) {
		t.Helper()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			var seen bool
			if err := owner.QueryRowContext(ctx, `SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE application_name = current_setting('search_path')
				AND pg_blocking_pids(pid) @> ARRAY[$1::integer] AND query LIKE $2)`,
				blockerPID, query).Scan(&seen); err != nil {
				t.Fatal(err)
			}
			if seen {
				return
			}
			select {
			case <-tick.C:
			case <-ctx.Done():
				t.Fatalf("%s never reached the held ledger: %v", what, ctx.Err())
			}
		}
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
	waitFor("WITH src AS MATERIALIZED%platformkit_handled%", "the copy of platformkit_handled")

	if _, err := owner.ExecContext(ctx,
		`SELECT platformkit_place_tenants('collect', '', 'late=collect', NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	result := <-moved
	// Either answer is safe, and the test says so rather than pinning one: the move
	// refuses because the membership it read is no longer the membership it holds, or it
	// renames both claims because its own reading already saw the placed tenant. What
	// neither answer may do is rename one of them and lose the other.
	if result.err != nil {
		if !errors.Is(result.err, events.ErrLedgerMoveContended) || result.report != (events.MoveReport{}) {
			t.Fatalf("the move answered %+v, want %v with no report", result, events.ErrLedgerMoveContended)
		}
		if got := ledgerShape(t, conn); got != before {
			t.Errorf("the refused move rewrote the ledgers:\n%s\nwant\n%s", got, before)
		}
		for _, id := range []uuid.UUID{early, late} {
			if n := len(movedRecords(t, conn, id)); n != 0 {
				t.Errorf("the refused move recorded %d ledger moves for tenant %s, want none", n, id)
			}
		}
		// The retry is the same walk over a membership that now holds still.
		result.report, result.err = events.MoveLedger(ctx, conn, "collect", "boot")
		if result.err != nil {
			t.Fatalf("the move after the placement: %v", result.err)
		}
	}
	if result.report.Claims != 2 || result.report.Tenants != 2 {
		t.Errorf("the move reported %+v, want two claims in two tenants", result.report)
	}
	for _, id := range []uuid.UUID{earlyEvent, lateEvent} {
		if got := durables(t, conn, "platformkit_handled", id); len(got) != 1 || got[0] != scoped {
			t.Errorf("the claim of %s sits at %v, want the one scoped durable %q", id, got, scoped)
		}
	}
}
