package db_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Whether an ending runs the work that waited for a rollback depends on what the
// database answered, not on what was asked for: a ROLLBACK the server refused leaves
// the outcome as unknown as a COMMIT that returned an error, and work meant for
// discarded rows must not run behind rows that may have been kept. These cases end
// the session a transaction lives on, from the owner role, so the ROLLBACK the kernel
// asks for can only be refused, and they ask what an effect that did run is handed.

// txSessionPID is the session a transaction runs on, read inside it.
func txSessionPID(t *testing.T, tx db.Tx[db.Tenant]) int {
	t.Helper()
	var pid int
	if err := tx.DB().Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
		t.Fatal(err)
	}
	return pid
}

// endSession asks the owner role to end that one session and waits until the server
// agrees it ended: pg_terminate_backend answers false for a session already gone.
func endSession(t *testing.T, admin *sql.DB, pid int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		var ended bool
		if err := admin.QueryRow("SELECT pg_terminate_backend($1)", pid).Scan(&ended); err != nil {
			t.Fatal(err)
		}
		if ended {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session %d was still standing after five seconds of pg_terminate_backend", pid)
}

func TestARollbackTheDatabaseRefusedRunsNeitherWaitingQueue(t *testing.T) {
	admin, app, acme := acmeThings(t)
	ctx, p, _ := lazy(t, app, acme)
	insertThrough(t, ctx, p, acme, "behind a session the server ended")
	var rolledBack, waited int
	mustDefer(t, ctx, db.AfterRollback, func(context.Context) error { rolledBack++; return nil })
	mustDefer(t, ctx, db.AfterCommit, func(context.Context) error { waited++; return nil })
	tx, err := p.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	endSession(t, admin, txSessionPID(t, tx))
	if err := p.Rollback(); err == nil {
		t.Fatal("Rollback answered nil for a session the server had ended: nothing was confirmed")
	}
	if rolledBack != 0 || waited != 0 {
		t.Errorf("%d effects waiting for a rollback and %d waiting for a commit ran on a ROLLBACK the database refused", rolledBack, waited)
	}
}

// The same ending reached from inside Run: the handler's own statement is refused
// because its session is gone, so Run rolls back and the database refuses that too.
func TestARollbackRefusedInsideRunRunsNeitherWaitingQueue(t *testing.T) {
	admin, app, acme := acmeThings(t)
	var rolledBack, waited int
	err := db.Run(tenancy.WithTenant(t.Context(), acme), app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		endSession(t, admin, txSessionPID(t, tx))
		mustDefer(t, ctx, db.AfterRollback, func(context.Context) error { rolledBack++; return nil })
		mustDefer(t, ctx, db.AfterCommit, func(context.Context) error { waited++; return nil })
		return tx.DB().Exec("SELECT 1").Error
	})
	if err == nil {
		t.Fatal("Run answered nil although its session was ended inside it")
	}
	if rolledBack != 0 || waited != 0 {
		t.Errorf("%d effects waiting for a rollback and %d waiting for a commit ran on a ROLLBACK the database refused", rolledBack, waited)
	}
}

// An effect that did run is handed the pool and the tenant of the work it follows.
// Whatever it writes goes on its own transaction, so it is its own commit — under
// that tenant's row-level security and never past it.
func TestARollbackEffectWritesUnderItsOwnTenantAndNoOther(t *testing.T) {
	admin, app, acme := acmeThings(t)
	other := newTenant("other")
	ctx, p, _ := lazy(t, app, acme)
	insertThrough(t, ctx, p, acme, "discarded")
	var ownWrite, foreignWrite error
	mustDefer(t, ctx, db.AfterRollback, func(ctx context.Context) error {
		conn, ok := db.ConnFrom(ctx)
		if !ok {
			t.Error("the effect was handed no pool to reach")
			return nil
		}
		ownWrite = db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return insert(tx.DB(), acme.ID, "written by the effect")
		})
		foreignWrite = db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return insert(tx.DB(), other.ID, "not this tenant's row")
		})
		return nil
	})
	if err := p.Rollback(); err != nil {
		t.Fatal(err)
	}
	if ownWrite != nil {
		t.Errorf("the effect could not write under its own tenant: %v", ownWrite)
	}
	if foreignWrite == nil {
		t.Error("the effect wrote another tenant's row from the context it was handed")
	}
	var kept, theirs int
	if err := admin.QueryRow("SELECT count(*) FROM things WHERE name = 'written by the effect'").Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow("SELECT count(*) FROM things WHERE tenant_id = $1", other.ID).Scan(&theirs); err != nil {
		t.Fatal(err)
	}
	if kept != 1 || theirs != 0 {
		t.Errorf("the effect's own row committed %d times and another tenant's %d times, want 1 and 0", kept, theirs)
	}
}
