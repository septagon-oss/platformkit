package db_test

import (
	"context"
	"errors"
	"testing"
)

func TestOwnerCancellationRollsBackTheReopenedTransactionOnly(t *testing.T) {
	admin, app, acme := acmeThings(t)
	base := app.Stats().InUse
	owner, p, cancelOwner := lazy(t, app, acme)
	t.Cleanup(func() { _ = p.Close(false) })
	insertThrough(t, owner, p, acme, "guard")
	if err := p.Close(true); err != nil {
		t.Fatalf("commit guard transaction: %v", err)
	}
	if n := committed(t, admin); n != 1 {
		t.Fatalf("rows after guard commit = %d, want 1", n)
	}

	// A streaming handler opens again after the guard transaction commits.
	// Its caller can outlive the request; its transaction cannot.
	caller := context.WithoutCancel(owner)
	insertThrough(t, caller, p, acme, "handler")
	cancelOwner()
	if err := p.Close(true); !errors.Is(err, context.Canceled) {
		t.Errorf("close reopened transaction = %v, want context.Canceled", err)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM things WHERE name = 'guard'"); n != 1 {
		t.Errorf("committed guard rows = %d, want 1", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM things WHERE name = 'handler'"); n != 0 {
		t.Errorf("committed handler rows = %d, want 0", n)
	}
	if _, err := p.Tx(caller); !errors.Is(err, context.Canceled) {
		t.Errorf("third transaction = %v, want context.Canceled", err)
	}
	if _, opened := p.Opened(); opened {
		t.Error("ended owner reopened a third transaction")
	}
	_ = p.Close(false)
	waitUntil(t, "the connection to return to the pool", func() bool { return app.Stats().InUse == base })
}
