package db_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// firstHold is how long the first namespace's run sits inside its own file, holding that
// namespace's composition key. It is long enough that a second namespace's run either
// overlaps it or plainly does not — the case asserts overlap on the server's own account,
// so this is a window it reads rather than a stopwatch it compares against — and short
// beside the five minutes a wait for a key that never comes back gets
// (migrationQueueBudget).
const firstHold = 15 * time.Second

// TestTwoNamespacesOfOneDatabaseMigrateAtOnce pins what the composition key is keyed to.
//
// The work two runs must not do at once is apply one file into one ledger, and a ledger
// lives in a namespace: `compositionLockKey` is the class of the lock, and the namespace
// the run's own session resolves to is its other half (kit/db/migrate.go). Keyed at the
// database instead, every migration run of every schema became one queue — which is what
// a suite of 529 schema boots, or two installations sharing a cluster, spends its wall
// clock on, and no guarantee is strengthened by it: two runs of one namespace still queue
// here, which the rest of this file's cases hold from a second session.
//
// What this case asserts is overlap rather than success. A run behind a key that is not
// its own simply waits — patiently, on the caller's context and not on a budget — and then
// succeeds, so "the second migration finished" proves nothing on its own. What proves it is
// the first run still being inside its file, still holding its key, at the moment the
// second one's table exists.
func TestTwoNamespacesOfOneDatabaseMigrateAtOnce(t *testing.T) {
	firstURL, _ := dbtest.URLs(t)
	first := dbtest.Open(t, firstURL)
	// The second namespace is the first one's name plus one character, so it carries the
	// same per-process id dbtest makes test names unique with and lands beside it rather
	// than on top of another package's schema.
	secondName := dbtest.DeploymentSchema(t, first) + "_b"
	t.Setenv("PLATFORMKIT_TEST_SCHEMA", secondName)
	secondURL, _ := dbtest.URLsFor(t)
	second := dbtest.Open(t, secondURL)

	firstDone := make(chan error, 1)
	firstCtx, cancelFirst := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Minute)
	defer cancelFirst()
	go func() {
		firstDone <- db.Migrate(firstCtx, firstURL,
			db.MigrationSource{Owner: "wide_a", Files: fstest.MapFS{
				"000001_slow.up.sql": {Data: []byte("CREATE TABLE wide_a (id bigint PRIMARY KEY); SELECT pg_sleep(" + strconv.Itoa(int(firstHold/time.Second)) + ")")},
			}})
	}()

	// Both reads are of the first run's own session: this file's holdsCompositionKey is
	// scoped to this schema's application_name, and a granted key of the composition class
	// held by that session is the thing the second run must not have to wait for.
	if why := waitUntil(t, migrationQueueBudget, firstDone,
		"the first namespace's run inside its own file",
		func() bool { return runIsInsideItsFile(t, first) && holdsCompositionKey(t, first) }); why != "" {
		t.Fatalf("the first run never stood inside its file holding its composition key: %s", why)
	}

	secondStarted := time.Now()
	// The short lock budget is what the tree this case refuses would answer with: a run
	// that reached a key belonging to another namespace would be waiting on a wait the
	// kernel promises to be patient about, and the loudest sign of that promise being
	// broken is the file's own budget refusing it.
	lock := time.Second
	secondCtx, cancelSecond := context.WithTimeout(context.WithoutCancel(t.Context()), migrationQueueBudget)
	defer cancelSecond()
	if err := db.MigrateWith(secondCtx, secondURL, db.MigrationBudget{LockTimeout: &lock},
		db.MigrationSource{Owner: "wide_b", Files: fstest.MapFS{
			"000001_quick.up.sql": {Data: []byte("CREATE TABLE wide_b (id bigint PRIMARY KEY)")},
		}}); err != nil {
		t.Fatalf("the second namespace's migration refused after %s while the first was inside its file: %v",
			time.Since(secondStarted).Round(time.Millisecond), err)
	}
	// The overlap, asserted rather than assumed: the first run's key is still granted and
	// its statement is still parked in pg_sleep, so the second run did not wait for it. That
	// pair of reads also says the first run has reported nothing yet — a run that had failed
	// or finished is out of its file — and its own result is read at the end of this case.
	if !holdsCompositionKey(t, first) {
		t.Fatal("the first run had already finished, so this case measured nothing: the second run proved nothing about the first one's key")
	}
	if !runIsInsideItsFile(t, first) {
		t.Fatal("the first run had left its file, so this case measured nothing")
	}
	var applied int
	scan(t, second, "SELECT count(*) FROM schema_migrations WHERE owner = 'wide_b'", &applied)
	if applied != 1 {
		t.Errorf("the second namespace applied %d of its files while the first held its own key, want 1", applied)
	}
	var inFirst int
	scan(t, first, "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace "+
		"WHERE c.relname = 'wide_b' AND n.oid = current_schema()::regnamespace", &inFirst)
	if inFirst != 0 {
		t.Errorf("the second namespace's table appeared in the first namespace: %d", inFirst)
	}

	if err := <-firstDone; err != nil {
		t.Errorf("the first namespace's migration: %v (reported contention: %t)", err, errors.Is(err, db.ErrContended))
	}
	scan(t, first, "SELECT count(*) FROM schema_migrations WHERE owner = 'wide_a'", &applied)
	if applied != 1 {
		t.Errorf("the first namespace applied %d of its files, want 1", applied)
	}
}
