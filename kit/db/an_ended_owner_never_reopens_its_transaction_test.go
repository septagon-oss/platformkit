package db_test

import (
	"context"
	"errors"
	"testing"
)

// The order the cancel/Close race only samples, held still: the owner ends
// after a write and before Close. The write is undone, Close says why, and no
// later caller, however long it lives, opens a second transaction.
func TestAnOwnerEndedAfterAWriteRollsItBackAndNeverReopens(t *testing.T) {
	admin, app, acme := acmeThings(t)
	base := app.Stats().InUse
	owner, p, cancelOwner := lazy(t, app, acme)
	insertThrough(t, owner, p, acme, "a")
	cancelOwner()

	if err := p.Close(true); !errors.Is(err, context.Canceled) {
		t.Errorf("Close(true) after the owner ended = %v, want context.Canceled", err)
	}
	if n := committed(t, admin); n != 0 {
		t.Errorf("committed rows = %d, want 0", n)
	}
	if _, err := p.Tx(context.WithoutCancel(owner)); !errors.Is(err, context.Canceled) {
		t.Errorf("Tx after the owner ended = %v, want context.Canceled", err)
	}
	if _, opened := p.Opened(); opened {
		t.Error("a Pending whose owner ended reports an open transaction")
	}
	waitUntil(t, "the connection to return to the pool", func() bool { return app.Stats().InUse == base })
}
