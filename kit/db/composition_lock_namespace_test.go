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

// TestTwoNamespacesOfOneDatabaseMigrateAtOnce is the case ADR 0005 does not cover: ADR 0005
// says one process migrates and the rest wait and find nothing to do, which is a guarantee
// about one namespace. This holds one namespace's key and asks what a second namespace of the
// same database pays for it — nothing — and it asks the question in the order that makes it a
// measurement: the key stays held across the second namespace's whole migration, is given
// back explicitly after that migration has been observed to finish, and only then does the
// namespace that held it get its own run out of the queue.
//
// The order is the assertion. Run the other way — release, then migrate — a run that queued
// the two namespaces anyway passes, because nothing in it had to happen while the key was
// held. So this case reads the queue in pg_locks rather than waiting on a clock, holds the
// key through the other namespace's completion, refuses the first run's answer if it arrived
// before the release, and then requires that same run to apply its file once the key is back.
func TestTwoNamespacesOfOneDatabaseMigrateAtOnce(t *testing.T) {
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
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(t.Context()), namespaceUnlockSQL, namespaceLockKey, own)
	}()

	source := db.MigrationSource{Owner: "namespaced", Files: fstest.MapFS{
		"000001_head.up.sql": {Data: []byte("CREATE TABLE namespaced_head (id bigint PRIMARY KEY)")},
	}}

	// The queue this case waits on, seen rather than timed. The held namespace's own run is
	// the one expected in it, and what bounds this wait is the caller's context and not any
	// budget (TestTheCompositionLockWaitsOnTheCallersContext pins which answer that gives).
	queued, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), queueWait)
	defer cancel()
	inQueue := make(chan error, 1)
	go func() {
		inQueue <- db.MigrateWith(queued, migrateURL, db.MigrationBudget{}, source)
	}()
	if !waitsOnOwnCompositionKey(t, holder, own) {
		t.Fatalf("no session of %s is queued for this namespace's composition key while it is held; two runs of one namespace must still queue, that being the whole of what the lock is for", own)
	}

	// The other namespace of the same database, whose key nobody holds. A subtest is how
	// this fixture asks for a second namespace of the same database (one namespace per
	// test name), and the body runs here, while the key above is still held. The bound is a
	// stopwatch on the answer and not part of the assertion: one file is a second's work,
	// and a run standing in a queue it has nothing to do in fails here rather than at the
	// end of a package's ten-minute watchdog.
	t.Run("another namespace of the same database", func(t *testing.T) {
		otherURL, _ := dbtest.URLs(t)
		other := schemaOf(t, otherURL)
		if other == own {
			t.Fatal("two namespaces of one database are wanted, and the fixture gave one")
		}
		applied := make(chan error, 1)
		go func() {
			applied <- db.MigrateWith(t.Context(), otherURL, db.MigrationBudget{}, source)
		}()
		select {
		case err := <-applied:
			if err != nil {
				t.Fatalf("the migration of %s refused while %s held its own key: %v", other, own, err)
			}
		case <-time.After(queueWait):
			t.Fatal("a migration whose own namespace holds no lock has not finished while another namespace's key is held; the key has no namespace in it again")
		}
	})

	// The first run is still in the queue at the moment the second one's migration is
	// complete: that is the two namespaces migrating at once, and it is the fact the release
	// below is only worth making after.
	select {
	case err := <-inQueue:
		t.Fatalf("the run of the held namespace answered %v while its key was still held and before it was given back; the release says nothing about a run that was never waiting", err)
	default:
	}

	if _, err := conn.ExecContext(t.Context(), namespaceUnlockSQL, namespaceLockKey, own); err != nil {
		t.Fatalf("give back the composition lock of %s: %v", own, err)
	}
	select {
	case err := <-inQueue:
		if err != nil {
			t.Fatalf("the run of the namespace whose key was held returned %v once the key was given back: %s", err, "the released queue did not reach the run it was holding")
		}
	case <-time.After(queueWait):
		t.Fatalf("the run of %s had not finished after its namespace's key was given back", own)
	}
	if n := countRows(t, holder, "SELECT count(*) FROM schema_migrations WHERE owner = 'namespaced'"); n != 1 {
		t.Fatalf("the run that reached the head of %s's queue applied %d history rows there, want 1", own, n)
	}
}

// queueWait bounds the two waits below and the one stopwatch above. It is not the assertion:
// every answer this case checks arrives on an observable (a row in pg_locks, a run returning)
// and a wait that runs out reports which of them did not, the way the case that already uses
// this bound for a single migration's completion does.
const queueWait = 30 * time.Second

// waitsOnOwnCompositionKey reads the queue rather than the clock: it answers once a session of
// the namespace the admin handle is connected to stands ungranted for that namespace's
// composition key, which is the state the case below needs before it can call anything the
// other namespace did a migration beside a held key. It answers false when the queue never
// forms within queueWait, leaving the caller to say which half of the lock has gone.
func waitsOnOwnCompositionKey(t *testing.T, admin sqlDB, namespace string) bool {
	t.Helper()
	deadline := time.Now().Add(queueWait)
	for {
		var queued int
		scan(t, admin, cancellationKeyWaiter, &queued)
		if queued > 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-t.Context().Done():
			t.Fatalf("waiting for %s's queue: %v", namespace, t.Context().Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}
