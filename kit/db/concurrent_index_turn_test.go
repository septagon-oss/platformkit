package db_test

// The composition lock keeps two replicas off each other's files, and an autocommit file
// gives that lock up for the length of its own statement. What the release leaves is two
// boots that each read the file as pending before either recorded it — and the object they
// then both work on is one index, not two. kit/db/concurrent_index_replicas_test.go races
// that overlap and reads the server's answer; the case below makes the overlap happen and
// watches it, so that what it asks about is the second boot's turn and not the timing of a
// green machine. The first boot is caught asleep inside its build holding the file's lock,
// alone and with one build counted, and what is then counted is the work: one boot built
// the index and the other found it, which is the release, not two boots fighting over it.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// serverDetail carries the server's own words about a lock wait out of the error: the
// DETAIL of 40P01 names the two sessions and the two locks of the cycle, and a case about
// two boots meeting is unreadable without them.
func serverDetail(err error) string {
	pg, isPostgres := errors.AsType[*pgconn.PgError](err)
	if !isPostgres || pg.Detail == "" {
		return ""
	}
	return "\nserver detail: " + pg.Detail
}

func TestABootThatReachesAFileAnotherBootIsBuildingWaitsForItsTurn(t *testing.T) {
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
	exec(`CREATE TABLE turn_items (id bigint NOT NULL)`)
	exec(`INSERT INTO turn_items VALUES (1)`)
	exec(`CREATE SEQUENCE turn_builds`)
	// The expression is marked immutable for no reason but to be called inside a real
	// concurrent build, as the other build fixtures here do. The sequence counts the builds
	// that reached it, and the sleep is what catches the first boot in the act: it is long
	// past the time a second boot needs to reach the same file.
	exec(`CREATE FUNCTION turn_key(value bigint) RETURNS bigint LANGUAGE plpgsql IMMUTABLE AS $$
BEGIN PERFORM nextval('turn_builds'); PERFORM pg_sleep(3); RETURN value; END $$`)
	const build = `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS turn_unique ON turn_items (turn_key(id));`
	source := mapSource("turn", "-- pkit: autocommit=true\n"+build)
	budget := db.MigrationBudget{LockTimeout: new(90 * time.Second)}

	building := make(chan error, 1)
	go func() { building <- db.MigrateWith(ctx, adminURL, budget, source) }()
	// The first boot is inside the build, which is the state the second must not repair
	// underneath: the index exists and is invalid until this sleep ends.
	waitFor(t, admin, ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
		WHERE application_name = current_setting('search_path') AND wait_event = 'PgSleep')`,
		"the first boot never reached its build")

	queued := make(chan error, 1)
	go func() { queued <- db.MigrateWith(ctx, adminURL, budget, source) }()
	// The first boot holds the file's own advisory lock — a namespaced one, so not the
	// composition lock, which puts 0 in classid — and holds it while it is asleep in the
	// build. This is the overlap: the second boot is already running, and one build has
	// reached the index.
	waitFor(t, admin, ctx, `
		SELECT (SELECT count(*) FROM pg_locks l JOIN pg_stat_activity b ON b.pid = l.pid
				WHERE l.locktype = 'advisory' AND l.classid <> 0
				  AND l.objid = hashtext('turn/1') AND l.granted
				  AND b.wait_event = 'PgSleep') = 1
			AND (SELECT last_value FROM turn_builds) = 1`,
		"the boot that was building the index did not hold the file's lock alone while it built")

	if err := <-building; err != nil {
		t.Fatalf("the boot that was building the index: %v%s", err, serverDetail(err))
	}
	if err := <-queued; err != nil {
		t.Fatalf("the boot that waited for its turn: %v%s", err, serverDetail(err))
	}
	var builds int64
	if err := admin.QueryRowContext(ctx, `SELECT last_value FROM turn_builds`).Scan(&builds); err != nil {
		t.Fatal(err)
	}
	if builds != 1 {
		t.Errorf("builds that reached the index = %d, want 1: the boot that waited rebuilt what it was waiting for", builds)
	}
	if rows := recordedFiles(t, admin, "turn"); rows != 1 {
		t.Errorf("history rows = %d, want 1", rows)
	}
	var valid bool
	if err := admin.QueryRowContext(ctx, `SELECT indisvalid FROM pg_index
		WHERE indexrelid = 'turn_unique'::regclass`).Scan(&valid); err != nil || !valid {
		t.Fatalf("index valid=%v, error=%v", valid, err)
	}
	exec(`INSERT INTO turn_items VALUES (1) ON CONFLICT DO NOTHING`)
	var rows int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM turn_items`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("certified index admitted %d duplicate rows", rows)
	}
}

// waitFor asks the database a question until it answers yes, and fails the test with the
// sentence naming what never happened. The question is one EXISTS over pg_stat_activity or
// pg_locks, so it is asked of the server rather than guessed from the client.
func waitFor(t *testing.T, admin *sql.DB, ctx context.Context, question, complaint string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var yes bool
		if err := admin.QueryRowContext(ctx, question).Scan(&yes); err != nil {
			t.Fatal(err)
		}
		if yes {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(complaint)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
