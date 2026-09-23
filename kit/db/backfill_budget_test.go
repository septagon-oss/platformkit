package db_test

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// backfill_budget_test.go is the case the drain's budget door made writable. A
// deployment names database.lock_timeout because a boot must not sit on a busy table,
// and the half of a release that sits longest is not the file — it is the fifty
// transactions of backfill behind it, each one waiting for a row the running
// application is holding. An operator who shortened the wait and got it for the file
// alone would have shortened the wrong thing, and nothing would have said so: the
// drain would simply have run on the default.
//
// The wait here is real, not simulated: another transaction holds the row the first
// window is about to write, and does not commit.

// TestTheDrainWaitsOnlyAsLongAsTheBudgetTheDeploymentNamed.
func TestTheDrainWaitsOnlyAsLongAsTheBudgetTheDeploymentNamed(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, passes integer NOT NULL DEFAULT 0);\nINSERT INTO probe (id) SELECT g FROM generate_series(1,25) g")},
	}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "fill", Files: files}); err != nil {
		t.Fatal(err)
	}
	files["000002_fill.up.sql"] = &fstest.MapFile{Data: []byte(`-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET passes = passes + 1 WHERE id IN (SELECT id FROM batch)`)}
	source := db.MigrationSource{Owner: "fill", Files: files}

	// Somebody is holding the row the first window will write.
	holder := dbtest.Open(t, migrateURL)
	conn, err := holder.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(t.Context(), "UPDATE probe SET passes = passes WHERE id = 1"); err != nil {
		t.Fatal(err)
	}

	budget := 250 * time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	started := time.Now()
	err = db.BackfillWith(ctx, migrateURL, db.MigrationBudget{LockTimeout: &budget}, source)
	waited := time.Since(started)
	if err == nil {
		t.Fatal("a drain whose batch could not take a row lock reported success")
	}
	if !errors.Is(err, db.ErrContended) {
		t.Errorf("the blocked drain returned %q; errors.Is(err, db.ErrContended) is what an operator's script reads", err)
		return
	}
	// Five seconds is the default this run refused to use. Three is comfortably above
	// 250ms of waiting and comfortably below the wait the configuration said no to.
	if waited > 3*time.Second {
		t.Errorf("the drain waited %s on a %s lock budget; the budget the deployment named never reached the batch", waited.Round(time.Millisecond), budget)
	}
	// A refusal writes nothing: the batch that could not take the lock rolled back
	// whole, the version is not applied, and nothing past it ran.
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT coalesce(sum(passes),0) FROM probe"); n != 0 {
		t.Errorf("the refused drain committed work to %d rows", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner='fill' AND version=2"); n != 0 {
		t.Errorf("a drain that never took its first lock wrote %d history rows", n)
	}
}
