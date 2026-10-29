package app

import (
	"context"
	"errors"

	"github.com/septagon-oss/platformkit/kit/db"
)

// migrate applies the ledger as the owner role. Every role does, worker
// included: the advisory lock makes the race safe, which removes the ordering
// problem instead of sequencing it. The budgets are the deployment's, if it named
// any, and the composition is app.Migrate — the same one `platformkit migrate`
// runs, so a retry is not a second definition of what migrating means.
// See docs/adr/0005.
func (a *App) migrate(ctx context.Context) error {
	err := Migrate(ctx, a.cfg, a.mods)
	if !errors.Is(err, db.ErrBackfillBudget) {
		return err
	}
	// One answer from that run is not a failed boot. A data file whose drain hit the
	// bound a migration gives itself says "the committed batches stand and the rest
	// is the worker's to drain", and the process reading that sentence is the one
	// that owns jobs.BackfillMigrations: refusing the boot here would stop the only
	// tick that can finish the work, which is the failure kit/db documents as
	// impossible. The batches that committed are committed and the cursor names where
	// the next batch starts, so this role starts, says what it left, and its tick
	// drains the rest. The same error out of `platformkit migrate` or Bootstrap is
	// still the caller's to read: those doors asked for a run that finishes.
	a.log.WarnContext(ctx, "app: left a data migration for the schema-backfill tick", "err", err)
	return nil
}
