package db_test

import (
	"context"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestACancelledMigrationHoldsNoCompositionLockOnceItReturns asks the property
// TestMigrationCancellationRollsBackAndReleasesTheLock names in its title without
// a clock and without the shared key's queue. The retry in that case waits on the
// composition key with every other package's migration, so its bound has to be
// patient — and a patient bound cannot tell "released" from "released when some
// lingering session finally went". This case reads pg_locks instead, filtered to
// the sessions of this test's own schema (dbtest makes application_name the
// schema), so nothing another package holds can be counted: once Migrate has
// returned its cancellation, the run's session gives the key back on its own
// teardown, not whenever a queue of other packages' migrations allows a retry in.
func TestACancelledMigrationHoldsNoCompositionLockOnceItReturns(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	source := db.MigrationSource{Owner: "slow", Files: fstest.MapFS{
		"1_slow.up.sql": {Data: []byte("CREATE TABLE slow (value int); SELECT pg_sleep(30)")},
	}}
	admin := dbtest.Open(t, migrateURL)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- db.Migrate(ctx, migrateURL, source) }()

	// Wait for the statement, not for a duration: the run is inside its file, so
	// it holds the key. The listener is the server's own activity view.
	for {
		var sleeping bool
		scan(t, admin, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE application_name = current_setting('search_path') AND wait_event = 'PgSleep')`, &sleeping)
		if sleeping {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("migration returned before it reached its SQL: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("migration ignored cancellation")
	}

	// The run's own session is the only thing this reads, so the bound is on that
	// session's teardown alone — measured at 3-4ms on a loaded workstation once the
	// cancellation reached the server — and no other package's queue can spend it.
	// Five seconds is three orders of magnitude above that and still well under the
	// thirty seconds the file's pg_sleep would hold the key if the cancellation
	// never reached the statement.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var held int
		scan(t, admin, `SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid
			WHERE l.locktype = 'advisory' AND l.granted
				AND ((l.classid::bigint << 32) | l.objid::bigint) = `+strconv.FormatInt(compositionLockKey, 10)+`
				AND a.application_name = current_setting('search_path')`, &held)
		if held == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d session(s) of the cancelled run still hold the composition lock 5s after Migrate returned", held)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
