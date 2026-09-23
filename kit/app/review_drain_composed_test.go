package app

// review_drain_composed_test.go is the case the migration runner's whole story
// about a data migration depends on and nothing else ran: kit/db deliberately
// leaves a drain over a table with readers to the worker, and kit/app is the
// composition that schedules it. Delete the line that schedules it and every
// gate in the repository stays green — the migration that deferred the work
// returns nil, the release is "applied", and the column stays empty forever.
// kit/app/jobs_test.go wrote the reason for this file already: "the composition
// that writes one has to schedule the job that empties it".

import (
	"context"
	"database/sql"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/module"
)

// drainRelease is the shape a second release of a module takes when it fills a
// column rather than adding one: the table exists and is applied, and the backfill
// is a phase=data file beside it.
func drainRelease() (module.Module, fstest.MapFS) {
	files := fstest.MapFS{
		"000001_rows.up.sql": {Data: []byte(`CREATE TABLE drain_rows (
	id bigint PRIMARY KEY,
	nickname text,
	marked integer NOT NULL DEFAULT 0
);
INSERT INTO drain_rows (id, nickname) SELECT g, 'row ' || g FROM generate_series(1, 25) g`)},
		"000002_mark.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=drain_rows
UPDATE drain_rows SET marked = marked + 1 WHERE id IN (SELECT id FROM batch)`)},
		"000003_note.up.sql": {Data: []byte("ALTER TABLE drain_rows ADD COLUMN note text")},
	}
	return module.Module{Name: "drainmod", Migrations: files}, files
}

// TestTheWorkerDrainsWhatTheMigrationLeftBehind walks the convergence the README
// describes, through the composition an installation actually boots: the release
// applies its schema and stops in front of the drain; the worker the same
// composition schedules finishes it; the next migration then applies the file that
// waited behind it.
func TestTheWorkerDrainsWhatTheMigrationLeftBehind(t *testing.T) {
	cfg, opts := compose(t)
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	mod, files := drainRelease()

	// An installation one release behind: the module's table is applied, so the
	// owner has history and the data file is a drain over a table with readers.
	history := module.Module{Name: mod.Name, Migrations: fstest.MapFS{"000001_rows.up.sql": files["000001_rows.up.sql"]}}
	if err := db.Migrate(t.Context(), cfg.Database.MigrateURL, MigrationSources([]module.Module{history})...); err != nil {
		t.Fatalf("the previous release: %v", err)
	}

	opts.Role = Worker
	opts.Transport = memory.New()
	a, err := New(t.Context(), cfg, []module.Module{mod}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	runCtx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(runCtx) }()

	// The drain is on a tick, not on boot: the job this composition schedules runs
	// every jobs.BackfillEvery, and the case waits for the work rather than for the
	// clock it happens to use.
	if !until(t.Context(), 45*time.Second, func() bool {
		return count(t, owner, "SELECT count(*) FROM drain_rows WHERE marked = 1") == 25
	}) {
		t.Fatalf("the worker never drained the data migration: %d of 25 rows marked, %d history rows, %d progress rows",
			count(t, owner, "SELECT count(*) FROM drain_rows WHERE marked = 1"),
			count(t, owner, "SELECT count(*) FROM schema_migrations WHERE owner='drainmod' AND version=2"),
			count(t, owner, "SELECT count(*) FROM schema_migration_backfill"))
	}
	if n := count(t, owner, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress rows outlived the drain", n)
	}
	if n := count(t, owner, "SELECT count(*) FROM drain_rows WHERE marked > 1"); n != 0 {
		t.Errorf("%d rows were written more than once by the drain", n)
	}
	if n := count(t, owner, "SELECT count(*) FROM pg_attribute WHERE attrelid='drain_rows'::regclass AND attname='note'"); n != 0 {
		t.Errorf("the file behind the pending drain applied before the drain ran")
	}

	// The next migration — what `platformkit migrate --drain` does in one step and
	// a deployment does across two ticks — now finds nothing in the way.
	if err := Migrate(t.Context(), cfg, []module.Module{mod}); err != nil {
		t.Fatalf("the migration after the drain: %v", err)
	}
	if n := count(t, owner, "SELECT count(*) FROM pg_attribute WHERE attrelid='drain_rows'::regclass AND attname='note'"); n != 1 {
		t.Errorf("the release behind a finished drain still did not apply")
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("worker Run: %v", err)
	}
}

// until waits for a condition on a bounded context.
func until(ctx context.Context, bound time.Duration, ok func() bool) bool {
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		if ok() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return ok()
}

// count is one scalar out of the owner connection the case asserts against.
func count(t *testing.T, owner *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := owner.QueryRowContext(t.Context(), query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}
