package db_test

// The four cases below are the pair for Pending's ending: Commit answers what
// Close(true) answered and Rollback what Close(false) did, decision by decision — a
// spelling that lost the effect queue would break the transactional outbox behind it.

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

// TestPendingCommitCommitsWhatCloseTrueCommitted: the row reaches another connection,
// and the deferred effect runs once, on the first Commit and none after.
func TestPendingCommitCommitsWhatCloseTrueCommitted(t *testing.T) {
	admin, conn, tenant := acmeThings(t)
	ctx, pending, _ := lazy(t, conn, tenant)
	insertThrough(t, ctx, pending, tenant, "committed")
	ran := 0
	if err := db.AfterCommit(ctx, func(context.Context) error { ran++; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := pending.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := pending.Commit(); err != nil {
		t.Errorf("a second Commit answered %v; the no-op Close(true) answers nil", err)
	}
	if ran != 1 {
		t.Errorf("the effect deferred to after the commit ran %d times, want once", ran)
	}
	if n := committed(t, admin); n != 1 {
		t.Errorf("rows visible to another connection = %d, want 1", n)
	}
}

// TestPendingRollbackRollsBackWhatCloseFalseRolledBack: the row never appears, the
// effect that waited for the commit is dropped with it, and the Pending reopens.
func TestPendingRollbackRollsBackWhatCloseFalseRolledBack(t *testing.T) {
	admin, conn, tenant := acmeThings(t)
	ctx, pending, _ := lazy(t, conn, tenant)
	insertThrough(t, ctx, pending, tenant, "rolled back")
	ran := 0
	if err := db.AfterCommit(ctx, func(context.Context) error { ran++; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := pending.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := pending.Rollback(); err != nil {
		t.Errorf("a second Rollback answered %v, want nil", err)
	}
	if ran != 0 {
		t.Errorf("an effect deferred to a rollback's commit ran %d times, want none", ran)
	}
	if n := committed(t, admin); n != 0 {
		t.Errorf("a rolled back transaction left %d rows behind", n)
	}
	insertThrough(t, ctx, pending, tenant, "after the rollback")
	if err := pending.Commit(); err != nil {
		t.Fatalf("the Pending could not open again after a rollback: %v", err)
	}
	if n := committed(t, admin); n != 1 {
		t.Errorf("the transaction after the rollback committed %d rows, want 1", n)
	}
}

// TestCommitUnderAnEndedOwnerWritesNothingTheWayCloseTrueDid: nothing commits under an
// owner that has ended, and the answer names that owner's own cause.
func TestCommitUnderAnEndedOwnerWritesNothingTheWayCloseTrueDid(t *testing.T) {
	admin, conn, tenant := acmeThings(t)
	ctx, pending, cancel := lazy(t, conn, tenant)
	insertThrough(t, ctx, pending, tenant, "orphan")
	ran := 0
	if err := db.AfterCommit(ctx, func(context.Context) error { ran++; return nil }); err != nil {
		t.Fatal(err)
	}
	cancel()
	err := pending.Commit()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Commit under an ended owner = %v, want an error wrapping its cause", err)
	}
	if ran != 0 || committed(t, admin) != 0 {
		t.Errorf("the refused commit ran %d effects and left %d rows", ran, committed(t, admin))
	}
}

// TestCommitRefusesATransactionWhoseSettingsWereRewrittenTheWayCloseDid: the settings
// re-read is inside the ending, so the friendlier spelling cannot commit past it.
func TestCommitRefusesATransactionWhoseSettingsWereRewrittenTheWayCloseDid(t *testing.T) {
	admin, conn, tenant := acmeThings(t)
	ctx, pending, _ := lazy(t, conn, tenant)
	insertThrough(t, ctx, pending, tenant, "escaped")
	ran := 0
	if err := db.AfterCommit(ctx, func(context.Context) error { ran++; return nil }); err != nil {
		t.Fatal(err)
	}
	tx, err := pending.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.DB().Exec(`SELECT set_config('platformkit.system_access', 'true', true)`).Error; err != nil {
		t.Fatal(err)
	}
	err = pending.Commit()
	if !errors.Is(err, db.ErrScopeTampered) {
		t.Fatalf("Commit of a rewritten transaction = %v, want ErrScopeTampered", err)
	}
	if ran != 0 || committed(t, admin) != 0 {
		t.Errorf("the refused commit ran %d effects and left %d rows", ran, committed(t, admin))
	}
}
