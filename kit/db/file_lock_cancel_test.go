package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestCancellingAFileLockWaitLeavesTheNextBootFree(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	admin := dbtest.Open(t, adminURL)
	ctx, stop := context.WithTimeout(t.Context(), 120*time.Second)
	defer stop()
	if _, err := admin.ExecContext(ctx, `CREATE TABLE cancelled_items (id bigint)`); err != nil {
		t.Fatal(err)
	}
	holder, err := admin.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	var taken bool
	if err := holder.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(7240102, hashtext('cancelled/1'))`).Scan(&taken); err != nil || !taken {
		t.Fatalf("take file lock: taken=%v err=%v", taken, err)
	}
	defer holder.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock_all()`)
	source := mapSource("cancelled", "-- pkit: autocommit=true\nCREATE INDEX CONCURRENTLY IF NOT EXISTS cancelled_index ON cancelled_items (id);")
	budget := db.MigrationBudget{LockTimeout: new(90 * time.Second)}
	waiting, cancel := context.WithCancel(ctx)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- db.MigrateWith(waiting, adminURL, budget, source) }()
	// Observe the wait itself before cancellation, independently of the error it returns.
	waitFor(t, admin, ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
		WHERE application_name = current_setting('search_path') AND state = 'idle'
		AND query = 'SELECT pg_try_advisory_lock($1, hashtext($2))')`, "boot never reached the idle file wait")
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled wait: %v, want context.Canceled", err)
		}
	case <-ctx.Done():
		t.Fatal("cancelled boot did not return")
	}
	if rows := recordedFiles(t, admin, "cancelled"); rows != 0 {
		t.Fatalf("cancelled boot recorded %d files", rows)
	}
	var absent bool
	if err := admin.QueryRowContext(ctx, `SELECT to_regclass('cancelled_index') IS NULL`).Scan(&absent); err != nil || !absent {
		t.Fatalf("cancelled boot left an object: absent=%v err=%v", absent, err)
	}
	if _, err := holder.ExecContext(ctx, `SELECT pg_advisory_unlock_all()`); err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateWith(ctx, adminURL, budget, source); err != nil {
		t.Fatalf("next boot: %v", err)
	}
	if rows := recordedFiles(t, admin, "cancelled"); rows != 1 {
		t.Fatalf("next boot recorded %d files, want 1", rows)
	}
	var valid bool
	if err := admin.QueryRowContext(ctx, `SELECT indisvalid FROM pg_index WHERE indexrelid = 'cancelled_index'::regclass`).Scan(&valid); err != nil || !valid {
		t.Fatalf("next boot index: valid=%v err=%v", valid, err)
	}
}
