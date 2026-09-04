package app

import (
	"context"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
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
// ordinary case here — a person who has just cloned the repository has run
// `make up` and nothing else — and it is the same ledger the application
// applies, modules included, because a bootstrap that migrated less than the
// process it is bootstrapping would leave a schema the app then has to finish.
//
// The lock makes "it refuses once any tenant exists" true of two bootstraps
// racing and not only of two run in sequence: both would otherwise read an
// empty tenants table in their own snapshot and both would create one, leaving
// two first tenants and two administrators who each believe they are the only
// one. pg_advisory_xact_lock serializes them, so the second reads the first
// one's tenant and refuses.
func Bootstrap(ctx context.Context, cfg config.Config, mods []module.Module, fn func(context.Context, db.Tx[db.System]) error) error {
	if err := db.Migrate(ctx, cfg.Database.MigrateURL, MigrationSources(mods)...); err != nil {
		return err
	}
	conn, err := db.Open(ctx, cfg.Database.URL)
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
