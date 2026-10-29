package jobs

import (
	"context"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
)

// BackfillEvery is how often the worker looks for a data migration an
// installation left half-done. It is a tick rather than a queue: the work is
// found in the ledger, not in a table of tasks, so a tick that finds nothing has
// read two tables and is done.
const BackfillEvery = 5 * time.Second

// BackfillJobName is the name the drain carries, and so the name its advisory lock
// is taken under (jobs.Job names itself). kit/app writes the job twice — the drain
// it can build from its configuration, and the one that refuses every tick when that
// configuration cannot declare what it says about itself — and one name is the
// difference between one job with two doors and two jobs nobody can tell apart.
const BackfillJobName = "schema-backfill"

// BackfillMigrations is the worker's half of a phase=data migration: Migrate
// refuses a drain over a table that already has readers, because a boot that
// drained one would hold the installation open for as long as the table takes,
// and this is the only role that can finish the work. It drains on a tick, one
// batch per transaction, and a failed run leaves the cursor where the last commit
// put it, so the next tick is the retry.
//
// One pass is bounded: a drain that reached the bound reports a body the cursor
// cannot bound rather than a long table, ends through ErrBackfillBudget and
// leaves the rest to the next tick. It is not Parallel — two workers draining one
// table each take a window and one finds its cursor compare-and-set refused, and
// It opens its own pinned owner connection, because the application role is
// revoked the runner's two tables, and carries its declaration with the drain: a
// data file that places rows runs here, not at the boot that left it pending.
func BackfillMigrations(every time.Duration, migrateURL string, budget db.MigrationBudget, decl db.Declaration, sources ...db.MigrationSource) Job {
	return Job{
		Name:  BackfillJobName,
		Every: every,
		Run: func(ctx context.Context, _ *db.Conn) error {
			return db.BackfillWithOptions(ctx, migrateURL, db.MigrateOptions{Budget: budget, Declaration: decl}, sources...)
		},
	}
}
