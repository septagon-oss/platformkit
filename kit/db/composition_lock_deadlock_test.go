package db_test

// One database carries every package of this suite, and a migration that asks for the
// composition's advisory lock can be the session PostgreSQL picks out of a deadlock it
// finds while other sessions are in one. Being picked is not a migration that failed:
// the cancelled statement is the only one the run had sent, so it holds nothing, and the
// cycle its cancellation ended is gone. A run that reported the server's words instead of
// asking again turned a state with nothing at stake into a failed boot — and every test
// fixture of this repository reaches the ask, so it did that as the suite got busier.
//
// The two cases below stand on one seam: a schema-local `pg_advisory_lock` shadow, the
// same one kit/db/autocommit_lock_reask_test.go puts on the second ask, asked here to
// answer 40P01 a fixed number of times. The counter is a sequence, because a sequence is
// the only count that survives the abort of the statement that drew it — an ordinary
// UPDATE of a row would roll back with the raise it is written beside, and the case would
// be counting asks it had already thrown away.

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"

	"github.com/jackc/pgx/v5/pgconn"
)

// recordedFiles is the ledger's answer for one owner, with "no ledger at all" read as
// nothing recorded: a run that never got the composition lock created nothing, and the
// question it is being asked is what it left behind, not whether a table is there.
func recordedFiles(t *testing.T, admin *sql.DB, owner string) int {
	t.Helper()
	var ledger sql.NullString
	if err := admin.QueryRowContext(t.Context(), `SELECT to_regclass('schema_migrations')`).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if !ledger.Valid {
		return 0
	}
	var rows int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM schema_migrations WHERE owner = $1`, owner).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

// pickedSource is the shadow, its counter, and the two files whose run is measured: the
// ask is the first thing the runner sends, so both files wait behind it and neither is
// applied by a run that does not come back with the lock.
func pickedSource(t *testing.T, adminURL string, raises string) (migrateURL string) {
	t.Helper()
	admin := dbtest.Open(t, adminURL)
	ctx := t.Context()
	for _, statement := range []string{
		`CREATE SEQUENCE lock_asks`,
		`CREATE FUNCTION pg_advisory_lock(key bigint) RETURNS void LANGUAGE plpgsql AS $$
DECLARE ask bigint;
BEGIN
  ask := nextval('lock_asks');
  ` + raises + `
  PERFORM pg_catalog.pg_advisory_lock(key);
END $$`,
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("the fixture's %s: %v", statement, err)
		}
	}
	// The migrate session has to resolve that shadow, which means pg_catalog last:
	// a function there is found before one in the schema whatever the path names first.
	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("options", query.Get("options")+",pg_catalog")
	parsed.RawQuery = query.Encode()
	if !strings.HasSuffix(query.Get("options"), ",pg_catalog") {
		t.Fatalf("the shadow is never on the migrate session's path: options=%s", query.Get("options"))
	}
	return parsed.String()
}

// TestAPickedCompositionLockAskAsksAgainIsTheRunThatAppliesTheFiles: picked twice, the run
// asks a third time, takes the lock, and applies what it came for. The count is the
// assertion as much as the files are — a run that refused the first picking answers the
// same two questions with the opposite words.
func TestAPickedCompositionLockAskAsksAgainIsTheRunThatAppliesTheFiles(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	migrateURL := pickedSource(t, adminURL, `IF ask <= 2 THEN
    RAISE EXCEPTION 'deadlock detected' USING ERRCODE = '40P01';
  END IF;`)

	if err := db.Migrate(t.Context(), migrateURL,
		mapSource("picked",
			`CREATE TABLE picked_probe (id bigint PRIMARY KEY)`,
			`CREATE TABLE picked_later (id bigint PRIMARY KEY)`)); err != nil {
		t.Fatalf("a run picked out of a deadlock twice refused instead of asking again: %v", err)
	}

	admin := dbtest.Open(t, adminURL)
	if applied := recordedFiles(t, admin, "picked"); applied != 2 {
		t.Errorf("the run that came back for the lock recorded %d of its 2 files", applied)
	}
	var asks int64
	if err := admin.QueryRowContext(t.Context(), `SELECT last_value FROM lock_asks`).Scan(&asks); err != nil {
		t.Fatal(err)
	}
	if asks != 3 {
		t.Errorf("the composition lock was asked for %d times, want 3: the two pickings and the wait that took it", asks)
	}
}

// TestACompositionLockPickedEveryTimeRefusesWithTheServersOwnWords bounds the retry. The
// shadow answers 40P01 to every ask, so the only way this case ends is the run giving up:
// an unbounded retry would sit in the wait until its context ran out and the case would
// hang rather than report, which is the difference between a bounded ask and a patient one.
// Five is one ask and the four re-asks `holdCompositionLock` is given; a runner that
// re-asks more or less often changes this number and the sentence beside it together.
func TestACompositionLockPickedEveryTimeRefusesWithTheServersOwnWords(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	migrateURL := pickedSource(t, adminURL, `RAISE EXCEPTION 'deadlock detected' USING ERRCODE = '40P01';`)

	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 60*time.Second)
	defer cancel()
	err := db.Migrate(ctx, migrateURL,
		mapSource("picked",
			`CREATE TABLE picked_probe (id bigint PRIMARY KEY)`,
			`CREATE TABLE picked_later (id bigint PRIMARY KEY)`))
	if err == nil {
		t.Fatal("a run the database picked out of every deadlock reported success")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "40P01" {
		t.Errorf("a run picked every time = %v, want the server's own 40P01", err)
	}
	if !strings.Contains(err.Error(), "db: migrate: lock") {
		t.Errorf("the refusal does not say where it happened: %v", err)
	}

	admin := dbtest.Open(t, adminURL)
	if applied := recordedFiles(t, admin, "picked"); applied != 0 {
		t.Errorf("a run that never got the composition lock recorded %d files", applied)
	}
	var asks int64
	if err := admin.QueryRowContext(t.Context(), `SELECT last_value FROM lock_asks`).Scan(&asks); err != nil {
		t.Fatal(err)
	}
	if asks != 5 {
		t.Errorf("the composition lock was asked for %d times, want 5 and no more: the first ask and four re-asks", asks)
	}
}
