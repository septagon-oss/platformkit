package app

// review3_drain_in_flight_boots_test.go is the third review's case for the one
// convergence kit/db/README.md states as a guarantee:
//
//	"An owner that already has history is a table under readers: Migrate stops
//	 there … and jobs.BackfillMigrations — composed into the worker by kit/app
//	 as schema-backfill — is what finishes it … A boot that refused a drain
//	 would stop the only role that can finish it."
//
// The last sentence is the reason Migrate returns nil in front of a data file it
// will not drain. It is also the sentence the resumed drain contradicts: a file
// with a progress row is put back in the plan (kit/db.planOwner, `continue`),
// drained inside the boot, and refused at installBackfillBatches with
// ErrBackfillBudget. app.Start returns that error, so Run returns it, and the
// process that was supposed to own the tick never reaches work() — the boot
// refuses the drain and, by refusing it, stops the only role that can finish it.
//
// The case is the worker the composition actually boots, over a drain with more
// work left than fifty single-row batches: what has to be true is that the
// worker is alive and its tick empties the table. Whether the boot migrates the
// rest of it on the way up, or leaves the in-flight drain to the tick like every
// other one, is the kernel's to decide; both answers pass this case.

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/module"
)

func TestADrainAlreadyInFlightIsFinishedByTheWorkerThatBoots(t *testing.T) {
	cfg, opts := compose(t)
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	files := fstest.MapFS{
		// One row per batch, so the bound the inline drain gives itself
		// (installBackfillBatches) is a number of rows this case can count.
		"000001_rows.up.sql": {Data: []byte("CREATE TABLE drain_rows (id bigint PRIMARY KEY, marked integer NOT NULL DEFAULT 0);\n" +
			"INSERT INTO drain_rows (id) SELECT g FROM generate_series(1, 120) g")},
		"000002_mark.up.sql": {Data: []byte("-- pkit: phase=data\n-- pkit: batch=1\n-- pkit: table=drain_rows\n" +
			"UPDATE drain_rows SET marked = marked + 1 WHERE id IN (SELECT id FROM batch)")},
	}
	// The state a half-finished backfill leaves, reached only through the doors:
	// a fresh installation drains its own data file during the migration and
	// stops at the bound the runner gives itself, which leaves the owner with
	// history, a progress row, and more work than fifty single-row batches.
	mod := module.Module{Name: "drainmod", Migrations: files}
	if err := db.Migrate(t.Context(), cfg.Database.MigrateURL, MigrationSources([]module.Module{mod})...); err == nil {
		t.Fatal("the inline drain ran unbounded; this case needs the state where it stops at its bound")
	}
	if n := count(t, owner, "SELECT count(*) FROM drain_rows WHERE marked = 1"); n != 50 {
		t.Fatalf("%d rows are marked before the worker boots, want the 50 batches the inline drain allows", n)
	}
	if n := count(t, owner, "SELECT count(*) FROM schema_migrations WHERE owner = 'drainmod' AND version = 1"); n != 1 {
		t.Fatalf("the drain that stopped at its bound left the owner with no applied history; this case is about an installed database")
	}

	// The worker, as an installation boots it: same composition, same role.
	opts.Role = Worker
	opts.Transport = memory.New()
	a, err := New(t.Context(), cfg, []module.Module{mod}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	runCtx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(runCtx) }()

	if !until(t.Context(), 60*time.Second, func() bool {
		return count(t, owner, "SELECT count(*) FROM drain_rows WHERE marked = 1") == 120
	}) {
		select {
		case err := <-done:
			t.Fatalf("the worker's Run returned %v with %d of 120 rows drained; the boot refused the drain in flight and stopped the role that owns jobs.BackfillMigrations",
				err, count(t, owner, "SELECT count(*) FROM drain_rows WHERE marked = 1"))
		default:
		}
		t.Fatalf("the worker never finished the drain: %d of 120 rows marked, %d progress row(s)",
			count(t, owner, "SELECT count(*) FROM drain_rows WHERE marked = 1"),
			count(t, owner, "SELECT count(*) FROM schema_migration_backfill"))
	}
	if n := count(t, owner, "SELECT count(*) FROM drain_rows WHERE marked > 1"); n != 0 {
		t.Errorf("%d rows were written more than once on the way", n)
	}
	if n := count(t, owner, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d progress rows outlived the drain", n)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("worker Run: %v", err)
	}
}
