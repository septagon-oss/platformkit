package db_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

func TestARolledBackTransactionNeverRunsItsEffectHoweverOftenItIsClosed(t *testing.T) {
	admin, conn, tenant := acmeThings(t)
	ctx, pending, _ := lazy(t, conn, tenant)
	t.Cleanup(func() { _ = pending.Close(false) })
	insertThrough(t, ctx, pending, tenant, "gone")
	ran := 0
	if err := db.AfterCommit(ctx, func(context.Context) error {
		ran++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_ = pending.Close(false)
	}
	if ran != 0 || committed(t, admin) != 0 {
		t.Errorf("effect ran %d times over %d committed rows, want none over none", ran, committed(t, admin))
	}
}
