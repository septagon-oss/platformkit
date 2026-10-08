// composition_lock_namespace_test.go pins the scope of the composition lock — which
// sessions the run of ADR 0005 makes everybody else wait for. The lock exists so one file
// is not applied twice, and one file is applied twice only by two sessions that write one
// history table. One history table belongs to one namespace: the one the run's search_path
// resolves to, which is the one every table its files create lands in. So the key carries
// that namespace, and two installations that share a database but not a namespace do not
// queue behind one another.
//
// They used to. The key was one number for the whole database, which is what
// `pg_advisory_lock` sees. Measured across a `make check`, 32 of the 33 live backends of
// one worktree's database stood in `Lock:advisory` at once — an average of 29 across the
// run, a single wait of 55 s — with nothing in the queue to protect: every waiting
// migration was writing its own ledger in its own schema, and dbtest gives each case a
// schema of one shared database. One package's ten-minute watchdog went into that queue.
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

// namespaceLockKey is kit/db's own first key (kit/db/migrate.go: compositionLockKey),
// spelled out here rather than borrowed, as the other cases in this package spell it.
const namespaceLockKey = 7240101

// namespaceLockSQL reaches for the composition lock of a namespace named as its argument,
// which is the lock kit/db reaches for (holdCompositionLock) with the argument set to the
// namespace the session resolves to.
const namespaceLockSQL = "SELECT pg_advisory_lock($1::int, $2::regnamespace::oid::int)"

const namespaceUnlockSQL = "SELECT pg_advisory_unlock($1::int, $2::regnamespace::oid::int)"

// TestTheCompositionLockIsOneNamespacesAndNotOneDatabases holds the key of one namespace
// and runs two migrations: one against that namespace, which must queue, and one against a
// namespace nobody holds, which must not. The first half is the guarantee ADR 0005 makes —
// one process migrating and the rest waiting and finding nothing to do; the second is the
// queue that guarantee never asked for.
func TestTheCompositionLockIsOneNamespacesAndNotOneDatabases(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	own := schemaOf(t, migrateURL)
	holder := dbtest.Open(t, migrateURL)
	conn, err := holder.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(t.Context(), namespaceLockSQL, namespaceLockKey, own); err != nil {
		t.Fatalf("take the composition lock of %s: %v", own, err)
	}

	source := db.MigrationSource{Owner: "namespaced", Files: fstest.MapFS{
		"000001_head.up.sql": {Data: []byte("CREATE TABLE namespaced_head (id bigint PRIMARY KEY)")},
	}}

	// This namespace, which somebody is in: the run has the whole of its history table to
	// itself only once that session is out, so it waits, and what bounds the wait is the
	// caller's context and not any budget (TestTheCompositionLockWaitsOnTheCallersContext
	// pins which answer that produces).
	queued, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 2*time.Second)
	defer cancel()
	if err := db.MigrateWith(queued, migrateURL, db.MigrationBudget{}, source); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a migration of the namespace whose lock is held returned %v; two runs of one namespace must still queue, that being the whole of what the lock is for", err)
	}

	// Another namespace — public, which is a namespace of this database that this run is
	// not in and that no test of this suite migrates, so holding its key says only that
	// somebody else's composition is being applied beside this one. This run's own key goes
	// back first: the half above needs it held and this one needs it free, and the point of
	// the pair is which of the two the run is waiting on.
	if _, err := conn.ExecContext(t.Context(), namespaceUnlockSQL, namespaceLockKey, own); err != nil {
		t.Fatalf("give back the composition lock of %s: %v", own, err)
	}
	if _, err := conn.ExecContext(t.Context(), namespaceLockSQL, namespaceLockKey, "public"); err != nil {
		t.Fatalf("take the composition lock of public: %v", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(t.Context()), namespaceUnlockSQL, namespaceLockKey, "public")
	}()

	// The bound is a stopwatch on the answer, not part of the assertion: one file is a
	// second's work, and a run that stands in a queue it has nothing to do in fails here
	// rather than at the end of a package's ten-minute watchdog.
	applied := make(chan error, 1)
	go func() {
		applied <- db.MigrateWith(t.Context(), migrateURL, db.MigrationBudget{}, source)
	}()
	select {
	case err := <-applied:
		if err != nil {
			t.Fatalf("the migration of the held namespace refused: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("a migration whose own namespace holds no lock has not finished in 30s while another namespace's key is held; the key has no namespace in it again")
	}
}
