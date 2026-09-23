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

// BackfillMigrations is the worker's half of a phase=data migration. Migrate stops in
// front of a drain over a table that already has readers — a boot that drained one
// would hold the installation open for as long as the table takes, and a boot that
// refused it would stop the only role that can finish the work. This is that role: it
// drains on a tick, one batch per transaction, and a cancelled or failed run leaves the
// cursor where the last commit put it, so the next tick is the retry. One pass is bounded: ten
// thousand windows is more table than a tick should empty, and a drain that reached the bound is
// reporting a body the cursor cannot bound rather than a table that is merely long — either way
// the tick ends, says so through ErrBackfillBudget, and the next tick continues.
//
// Not Parallel, on purpose: two workers draining one table would each take a window and
// one would find its compare-and-set on the cursor refused — correct, and wasteful, and
// the advisory lock this job takes by name is the cheaper answer. A drain holds that lock
// for one pass over one table's key space, the shape the outbox relay's pass has.
//
// The connection the scheduler hands a job is the application's, and this job does not
// use it: the runner's two tables are revoked from the application role, so the drain
// opens its own pinned owner connection from migrateURL, as Migrate does. The budget
// is the one the deployment configured, because the drain is the half of a release that
// waits longest behind the running application's rows.
func BackfillMigrations(every time.Duration, migrateURL string, budget db.MigrationBudget, sources ...db.MigrationSource) Job {
	return Job{
		Name:  "schema-backfill",
		Every: every,
		Run: func(ctx context.Context, _ *db.Conn) error {
			return db.BackfillWith(ctx, migrateURL, budget, sources...)
		},
	}
}
