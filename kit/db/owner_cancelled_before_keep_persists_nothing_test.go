package db_test

import (
	"context"
	"errors"
	"testing"
)

func TestAnOwnerCancelledBeforeKeepPersistsNoWriteOfAnyCaller(t *testing.T) {
	admin, app, acme := acmeThings(t)
	base := app.Stats().InUse
	owner, p, cancelOwner := lazy(t, app, acme)
	t.Cleanup(func() { _ = p.Close(false) })

	first, cancelFirst := context.WithCancel(owner)
	insertThrough(t, first, p, acme, "first")
	cancelFirst()
	insertThrough(t, context.WithoutCancel(owner), p, acme, "second")

	cancelOwner()
	if err := p.Close(true); !errors.Is(err, context.Canceled) {
		t.Errorf("keep after owner cancel = %v, want context.Canceled", err)
	}
	if n := committed(t, admin); n != 0 {
		t.Errorf("committed rows = %d, want 0", n)
	}
	waitForPoolStat(t, "the connection to return to the pool", func() bool { return app.Stats().InUse == base })
}
