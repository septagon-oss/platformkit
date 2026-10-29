package db_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

func TestARolledBackEffectIsNotRunByALaterCommit(t *testing.T) {
	admin, conn, tenant := acmeThings(t)
	ctx, pending, _ := lazy(t, conn, tenant)
	t.Cleanup(func() { _ = pending.Close(false) })
	insertThrough(t, ctx, pending, tenant, "rolled back")
	var rolledBack, committedEffect int
	if err := db.AfterCommit(ctx, func(context.Context) error {
		rolledBack++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := pending.Close(false); err != nil {
		t.Fatal(err)
	}
	if rolledBack != 0 || committed(t, admin) != 0 {
		t.Fatal("the rollback left an effect or a row")
	}

	// A live request can open another transaction through the same Pending.
	insertThrough(t, ctx, pending, tenant, "committed")
	if err := db.AfterCommit(ctx, func(context.Context) error {
		committedEffect++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := pending.Close(true); err != nil {
		t.Fatal(err)
	}
	if got := committed(t, admin); got != 1 {
		t.Errorf("committed rows = %d, want only the second transaction's row", got)
	}
	if rolledBack != 0 || committedEffect != 1 {
		t.Errorf("effects after reopening: rolled back = %d, committed = %d; want 0 and 1", rolledBack, committedEffect)
	}
}
