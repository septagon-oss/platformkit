package app

import (
	"context"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
)

// bootstrapToken is the capability the first write of a new installation needs.
// There is no tenant yet, so there is no tenant transaction to do it in.
var bootstrapToken = syscap.NewSystemToken("bootstrap an empty installation")

// Bootstrap migrates the database and runs fn in one cross-tenant transaction.
//
// It is the only way an installation with no tenants gets its first one: every
// other write in the application happens inside a tenant, and a tenant is what
// this creates. The migration is part of it because an empty database is the
// ordinary case here — a person who has just cloned the repository has run
// `make up` and nothing else.
func Bootstrap(ctx context.Context, cfg config.Config, fn func(context.Context, db.Tx[db.System]) error) error {
	src, err := sources(nil)
	if err != nil {
		return err
	}
	if err := db.Migrate(ctx, cfg.Database.MigrateURL, src); err != nil {
		return err
	}
	conn, err := db.Open(ctx, cfg.Database.URL)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := db.RunSystem(ctx, conn, bootstrapToken, fn); err != nil {
		return fmt.Errorf("app: bootstrap: %w", err)
	}
	return nil
}
