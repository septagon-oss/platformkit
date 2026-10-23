package db_test

import (
	"testing"
)

func TestARolledBackTransactionIsNotCommittedByALaterKeep(t *testing.T) {
	admin, app, acme := acmeThings(t)
	owner, p, cancelOwner := lazy(t, app, acme)
	t.Cleanup(cancelOwner)
	insertThrough(t, owner, p, acme, "refused")

	if err := p.Close(false); err != nil {
		t.Fatalf("Close(false) = %v, want nil", err)
	}
	if err := p.Close(true); err != nil {
		t.Errorf("Close(true) after a rollback = %v, want nil: nothing is open to finalise", err)
	}
	if _, opened := p.Opened(); opened {
		t.Error("a closed Pending reports an open transaction")
	}
	if n := committed(t, admin); n != 0 {
		t.Errorf("committed rows = %d, want 0", n)
	}
	waitForPoolStat(t, "the connection to return to the pool", func() bool { return app.Stats().InUse == 0 })
}
