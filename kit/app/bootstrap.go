package app

import (
	"context"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/module"
)

// bootstrapToken is the capability the first write of a new installation needs.
// There is no tenant yet, so there is no tenant transaction to do it in.
var bootstrapToken = syscap.NewSystemToken("bootstrap an empty installation")

// bootstrapLock is the advisory lock two bootstraps of one empty installation
// contend for. It is transaction-scoped, so it is released by the commit or the
// rollback and there is nothing to unlock.
const bootstrapLock = "platformkit:bootstrap"

// Bootstrap migrates the database and runs fn in one cross-tenant transaction.
//
// It is the only way an installation with no tenants gets its first one: every
// other write in the application happens inside a tenant, and a tenant is what
// this creates. The migration is part of it because an empty database is the
// ordinary case here, and it is the same ledger the application applies, modules
// included, so a bootstrap never leaves a schema the app then has to finish.
//
// pg_advisory_xact_lock makes "it refuses once any tenant exists" true of two
// bootstraps racing and not only of two run in sequence: without it both would
// read an empty tenants table in their own snapshot and both would create a first
// tenant, with two administrators each believing they are the only one.
func Bootstrap(ctx context.Context, cfg config.Config, mods []module.Module, fn func(context.Context, db.Tx[db.System]) error) error {
	pool := databasePool(cfg.Database)
	if err := pool.Validate(); err != nil {
		return fmt.Errorf("app: %w", err)
	}
	if err := db.MigrateWithOptions(ctx, cfg.Database.MigrateURL, db.MigrateOptions{Budget: migrationBudget(cfg.Database)}, MigrationSources(mods)...); err != nil {
		return err
	}
	// New publishers require expansions that can follow an older data window.
	// Finish the ordinary drain/migrate sequence before the first domain write.
	if err := Drain(ctx, cfg, mods); err != nil {
		return err
	}
	if err := Migrate(ctx, cfg, mods); err != nil {
		return err
	}
	// The bootstrap publishes — tenant.created, through events.PublishFor — and it
	// does so before any composition was built. An outbox row that no subscriber can
	// receive is the failure kit/events/checkDeclared exists to refuse, so the
	// bootstrap says what it can emit before it emits it, from the same manifests
	// app.New will read a moment later. It names no app, because a first tenant belongs
	// to whichever composition bootstraps the installation, and carries the value on
	// its own context: fn's transaction publishes under this manifest — the migration,
	// the drain and the module migrate above it write schema and no event — and the
	// claim ends with the bootstrap rather than standing in the process for the next
	// bootstrapper to trip over.
	catalog, err := events.NewCatalog("", declaredEvents(withKernel(mods)))
	if err != nil {
		return err
	}
	defer catalog.Close()
	ctx = events.WithPublisher(ctx, catalog)
	conn, err := db.OpenWithPool(ctx, cfg.Database.URL, pool)
	if err != nil {
		return err
	}
	defer conn.Close()
	err = db.RunSystem(ctx, conn, bootstrapToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		if err := tx.DB().Exec("SELECT pg_advisory_xact_lock(hashtext(?)::bigint)", bootstrapLock).Error; err != nil {
			return fmt.Errorf("take the bootstrap lock: %w", err)
		}
		return fn(ctx, tx)
	})
	if err != nil {
		return fmt.Errorf("app: bootstrap: %w", err)
	}
	return nil
}

// migrationBudget resolves the two optional migration budgets at the same
// application boundary the pool limits use. kit/db owns the defaults and the
// meaning of a nil field, exactly as it owns the pool's.
func migrationBudget(cfg config.Database) db.MigrationBudget {
	budget := db.DefaultMigrationBudget()
	if cfg.LockTimeout != nil {
		budget.LockTimeout = cfg.LockTimeout
	}
	if cfg.StatementTimeout != nil {
		budget.StatementTimeout = cfg.StatementTimeout
	}
	return budget
}

// databasePool resolves optional configuration at the application boundary.
// The reusable DB owner remains the sole owner of default values and validation.
func databasePool(cfg config.Database) db.Pool {
	pool := db.DefaultPool()
	if cfg.MaxOpenConns != nil {
		pool.MaxOpenConns = *cfg.MaxOpenConns
	}
	if cfg.MaxIdleConns != nil {
		pool.MaxIdleConns = *cfg.MaxIdleConns
	}
	if cfg.ConnMaxLifetime != nil {
		pool.ConnMaxLifetime = *cfg.ConnMaxLifetime
	}
	return pool
}
