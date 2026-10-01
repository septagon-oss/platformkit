package app

import (
	"context"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/appname"
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
	decl, err := migrationDeclaration(cfg)
	if err != nil {
		return err
	}
	return db.MigrateDeclaring(ctx, cfg.Database.MigrateURL, migrationBudget(cfg.Database), decl, MigrationSources(mods)...)
}

// migrationDeclaration is what this boot tells the migration about itself, checked
// before a connection is opened. The slug is the one every shared name is formed
// from, so it goes through the same door as the transport's (config.NATS.AppName)
// and a broken one is refused here rather than at the file that reads it — where
// the refusal would arrive after the composition lock and look like a bad
// migration rather than a bad setting.
//
// The app pointer is nil when the configuration names no slug, which is a
// different statement from declaring the empty slug: the migration reads nil as "a
// boot that said nothing", and it places nothing on that. A deployment of one app
// migrating a database that already holds tenants names itself (nats.app set to its
// slug, or app.tenant_apps naming where each tenant goes) and is placed; a
// deployment that says nothing gets the list of tenants it cannot name.
func migrationDeclaration(cfg config.Config) (db.Declaration, error) {
	decl := db.Declaration{Hosts: cfg.App.Hosts, Tenants: cfg.App.TenantApps}
	slug, err := cfg.NATS.AppName()
	if err != nil {
		return db.Declaration{}, err
	}
	// Always declared: this is the composition's own boot, so it knows what it is
	// even when what it is has no slug. The empty string says "the deployment of one
	// app", which migrations/000034 reads as an app and places against; the nil
	// that db.MigrateDeclaring's default carries says "a boot that named itself
	// nothing", which is what a caller that is not a composition's boot declares.
	own := slug.String()
	decl.App = &own
	for tenant, app := range cfg.App.TenantApps {
		if _, err := appname.Parse(app); err != nil {
			return db.Declaration{}, fmt.Errorf("app.tenant_apps[%s]: %w", tenant, err)
		}
	}
	return decl, nil
}

// Drain finishes the data migrations a release left half-drained, which is the
// worker's job on a tick (jobs.BackfillMigrations) and this door for a rehearsal or
// an operator who is not going to wait for the tick. It calls the same db.BackfillWith
// the worker calls, over the same sources the composition selected and with the same
// budgets the configuration named, which is what makes a measurement of it worth
// anything: the step that reports what a drain cost is not free to run it on different
// patience from the one that will run it in production.
func Drain(ctx context.Context, cfg config.Config, mods []module.Module) error {
	return db.BackfillWith(ctx, cfg.Database.MigrateURL, migrationBudget(cfg.Database), MigrationSources(mods)...)
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
