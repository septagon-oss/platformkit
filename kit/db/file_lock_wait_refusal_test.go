package db_test

// An autocommit file runs under its own advisory lock (kit/db/migrate.go's holdFileLock),
// and a boot that reaches a file another boot is running waits for it with the session
// idle between asks, bounded by the caller's context. The case below pins the bounded
// half: a boot whose context runs out while another session holds the file's lock comes
// back with that context's own refusal, records no history row and builds no object —
// and the same file applies cleanly once the lock goes back, so the refusal left nothing
// behind that a later boot would trip over.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// fileLockSpace is kit/db's own, unexported, constant (kit/db/migrate.go): the classid
// namespacing the advisory lock an autocommit file takes for itself.
const fileLockSpace = 7240102

func TestABootThatRunsOutOfTimeWaitingForAFileWritesNothing(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	admin := dbtest.Open(t, adminURL)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	exec := func(statement string) {
		t.Helper()
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE held_items (id bigint NOT NULL)`)
	exec(`INSERT INTO held_items VALUES (1)`)
	const build = `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS held_unique ON held_items (id);`
	source := mapSource("held", "-- pkit: autocommit=true\n"+build)
	budget := db.MigrationBudget{LockTimeout: new(90 * time.Second)}

	// The holding boot is one pinned session, not the pool: an advisory lock belongs to
	// the connection that took it, and a pooled statement lands where the pool pleases.
	holder, err := admin.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	var taken bool
	if err := holder.QueryRowContext(ctx,
		`SELECT pg_try_advisory_lock($1, hashtext($2))`, fileLockSpace, "held/1").Scan(&taken); err != nil {
		t.Fatal(err)
	}
	if !taken {
		t.Fatal("the fixture could not take the file lock first")
	}
	// The reachability probe asks the server, not the refusal: the lock the boot below
	// will wait for is granted, which is the fixed behaviour's precondition.
	var granted bool
	if err := admin.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks
		WHERE locktype = 'advisory' AND classid <> 0
		  AND objid = hashtext('held/1') AND granted)`).Scan(&granted); err != nil {
		t.Fatal(err)
	}
	if !granted {
		t.Fatal("the file lock the fixture took is not granted in pg_locks")
	}

	hurried, cancelHurried := context.WithTimeout(ctx, 2*time.Second)
	defer cancelHurried()
	if err := db.MigrateWith(hurried, adminURL, budget, source); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a boot that ran out of time waiting for a held file = %v, want its own deadline", err)
	}
	if rows := recordedFiles(t, admin, "held"); rows != 0 {
		t.Errorf("history rows after the refusal = %d, want 0", rows)
	}
	var index sql.NullString
	if err := admin.QueryRowContext(ctx, `SELECT to_regclass('held_unique')`).Scan(&index); err != nil {
		t.Fatal(err)
	}
	if index.Valid {
		t.Errorf("a refused boot left %s behind, want no object at all", index.String)
	}

	var released bool
	if err := holder.QueryRowContext(ctx,
		`SELECT pg_advisory_unlock($1, hashtext($2))`, fileLockSpace, "held/1").Scan(&released); err != nil {
		t.Fatal(err)
	}
	if !released {
		t.Fatal("the fixture could not give the file lock back")
	}
	if err := db.MigrateWith(ctx, adminURL, budget, source); err != nil {
		t.Fatalf("the boot after the lock went back: %v", err)
	}
	if rows := recordedFiles(t, admin, "held"); rows != 1 {
		t.Errorf("history rows after the lock went back = %d, want 1", rows)
	}
	var valid bool
	if err := admin.QueryRowContext(ctx, `SELECT indisvalid FROM pg_index
		WHERE indexrelid = 'held_unique'::regclass`).Scan(&valid); err != nil || !valid {
		t.Fatalf("index valid=%v, error=%v", valid, err)
	}
}
