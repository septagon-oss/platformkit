package app

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/migrations"
)

// Migrate applies what the selected modules have not applied yet, as the owner
// role, with the budgets the configuration names and kit/db's documented
// defaults where it names none.
//
// It is one composition of sources and budgets rather than two, because the
// retry an operator runs after a contended file has to be the same run the boot
// would have done: the same modules, the same floors, the same patience. Every
// role's boot calls it (ADR 0005) and so does `platformkit migrate`, which is
// the door for the file that was contended — a process that migrates and exits,
// for somebody who is not deploying.
func Migrate(ctx context.Context, cfg config.Config, mods []module.Module) error {
	return db.MigrateWith(ctx, cfg.Database.MigrateURL, migrationBudget(cfg.Database), MigrationSources(mods)...)
}

// Drain finishes the data migrations a release left half-drained, which is the
// worker's job on a tick (jobs.BackfillMigrations) and this door for a rehearsal or
// an operator who is not going to wait for the tick. It calls the same
// db.Backfill the worker calls, over the same sources the composition selected,
// which is what makes a measurement of it worth anything.
func Drain(ctx context.Context, cfg config.Config, mods []module.Module) error {
	return db.Backfill(ctx, cfg.Database.MigrateURL, MigrationSources(mods)...)
}

// MigrationSources keeps the foundation first and modules in composition order.
// Module names own their histories; no global version allocation is needed. A
// module's Adopts and its rule floor travel with its files: an installation migrated
// before the module owned its SQL is re-owned on the way past and nothing re-runs, and
// versions already applied under those bytes are not judged by a rule they cannot fix.
func MigrationSources(mods []module.Module) []db.MigrationSource {
	sources := []db.MigrationSource{migrations.Source}
	for _, mod := range mods {
		if mod.Migrations != nil {
			sources = append(sources, db.MigrationSource{Owner: mod.Name, Files: mod.Migrations, Adopts: mod.Adopts, RulesFrom: mod.RulesFrom})
		}
	}
	return sources
}
