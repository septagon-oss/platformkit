package db_test

// A file that runs outside the transaction gives the composition's advisory lock up
// for the length of its one statement, so a CONCURRENTLY build waits for the
// transactions already in the database instead of queueing behind the boot of every
// other replica. When that statement is over the run asks for the lock again — and
// the wait it gets has to be the patient one the README promises for the lock, not
// the file's five seconds, or the release that adds an autocommit file makes every
// installation that runs two migrations at once refuse a migration that was fine.

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func mapSource(owner string, files ...string) db.MigrationSource {
	all := make(map[string]*fstest.MapFile, len(files))
	for i, body := range files {
		all[fmt.Sprintf("%06d_gate.up.sql", i+1)] = &fstest.MapFile{Data: []byte(body)}
	}
	return db.MigrationSource{Owner: owner, Files: fstest.MapFS(all)}
}

// TestTheCompositionLockComingBackIsNotBoundedByTheFileBudget holds a gate shut for
// longer than the run's lock budget, and opens it again: the run that took its
// budgets off for the wait comes back and applies the file, and the run that left
// them on refuses at its budget with the file unapplied.
func TestTheCompositionLockComingBackIsNotBoundedByTheFileBudget(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	admin := dbtest.Open(t, adminURL)
	ctx := t.Context()
	// `lock_gate` is what the second ask waits behind; `lock_asks` counts the asks,
	// so the first one — the patient wait every run begins with, before any budget is
	// on the session — stays out of the case and only the re-ask is measured.
	for _, statement := range []string{
		`CREATE TABLE lock_asks (id integer PRIMARY KEY, asks integer NOT NULL)`,
		`INSERT INTO lock_asks VALUES (1, 0)`,
		`CREATE TABLE lock_gate (kept integer)`,
		`INSERT INTO lock_gate VALUES (1)`,
		// The schema-local shadow, reached because this test's own search_path names
		// pg_catalog last: the first call records the ask, the second waits for the gate.
		`CREATE FUNCTION pg_advisory_lock(key bigint) RETURNS void LANGUAGE plpgsql AS $$
DECLARE step integer;
BEGIN
  UPDATE lock_asks SET asks = asks + 1 WHERE id = 1 RETURNING asks INTO step;
  IF step > 1 THEN
    LOCK TABLE lock_gate IN ACCESS EXCLUSIVE MODE;
  END IF;
END $$`,
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("the fixture's %s: %v", statement, err)
		}
	}

	// The migrate session resolves that shadow. Same URL, pg_catalog moved last.
	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("options", query.Get("options")+",pg_catalog")
	parsed.RawQuery = query.Encode()

	// The gate is shut before the run starts and open again a whole lock budget
	// later, so a run that refuses on the way is refusing on the wait and nothing else.
	gate, err := admin.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	tx, err := gate.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `LOCK TABLE lock_gate IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	release := func() { _ = tx.Rollback() }
	defer release()
	go func() {
		time.Sleep(3 * time.Second)
		release()
	}()

	lock := 400 * time.Millisecond
	migrateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	started := time.Now()
	err = db.MigrateWith(migrateCtx, parsed.String(), db.MigrationBudget{LockTimeout: &lock},
		mapSource("gate",
			`CREATE TABLE gate_probe (id bigint PRIMARY KEY, sent boolean NOT NULL DEFAULT false);`,
			`-- pkit: autocommit=true
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS gate_sent_once ON gate_probe (id) WHERE sent;`))
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("the autocommit file refused while the composition lock was held by someone else: %v", err)
	}
	if elapsed < 2*time.Second {
		t.Errorf("the run came back in %s, inside the 3s the gate was shut: it did not wait for the lock", elapsed)
	}
	var asks int
	if err := admin.QueryRowContext(ctx, `SELECT asks FROM lock_asks WHERE id = 1`).Scan(&asks); err != nil {
		t.Fatal(err)
	}
	if asks != 2 {
		t.Errorf("the release asked for the composition lock %d times, want twice: once to begin and once to come back", asks)
	}
	var applied int
	if err := admin.QueryRowContext(ctx,
		`SELECT count(*) FROM schema_migrations WHERE owner = 'gate' AND version = 2`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Errorf("the autocommit file is not in the ledger after a run that reported no error")
	}
	if held := strings.TrimSpace(query.Get("options")); !strings.HasSuffix(held, "pg_catalog") {
		t.Errorf("the shadow was never on the path: options=%s", held)
	}
}
