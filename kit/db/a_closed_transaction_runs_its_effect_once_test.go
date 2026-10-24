package db_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

func TestAClosedTransactionRunsItsEffectOnceHoweverOftenItIsClosed(t *testing.T) {
	admin, conn, tenant := acmeThings(t)
	ctx, pending, _ := lazy(t, conn, tenant)
	t.Cleanup(func() { _ = pending.Close(false) })
	insertThrough(t, ctx, pending, tenant, "once")
	ran := 0
	if err := db.AfterCommit(ctx, func(context.Context) error {
		ran++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := pending.Close(true); err != nil {
			t.Fatal(err)
		}
	}
	if ran != 1 || committed(t, admin) != 1 {
		t.Errorf("effect ran %d times over %d committed rows, want once over one", ran, committed(t, admin))
	}
}
