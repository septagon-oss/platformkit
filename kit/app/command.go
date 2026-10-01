package app

import (
	"context"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
)

// commandToken is the capability an operator command opens its cross-tenant
// transaction with. It says what the transaction is for, which is the whole
// reason a token exists: the log line RunSystem writes then names the command
// path that opened it, and nothing else can.
var commandToken = syscap.NewSystemToken("an operator command run against one tenant")

// RunCommand opens a cross-tenant transaction for one operator command and hands
// it to fn, then closes the connection.
//
// An application's own command cannot do this for itself: the system token is
// minted under kit/internal/syscap, which is internal to the kernel precisely so
// that no module and no composition can open a cross-tenant transaction by
// asking for one. What a command needs the door for is resolving a tenant by its
// slug — a control-plane read — and then handing that tenant to db.InTenant,
// which lends a tenant view of the same transaction to the work. The command
// therefore sees a system handle and a tenant view, never a way to write another
// tenant's rows: the widening stops at InTenant, which puts cross-tenant access
// back only after the tenant view has been taken away.
//
// It does not migrate, and it needs no module list for that: a command runs
// against the installation the operator already has, and a schema this binary
// expects but the database does not is `platformkit migrate`'s answer, not a
// surprise write buried in a read.
func RunCommand(ctx context.Context, cfg config.Config, fn func(context.Context, db.Tx[db.System]) error) error {
	pool := databasePool(cfg.Database)
	if err := pool.Validate(); err != nil {
		return fmt.Errorf("app: %w", err)
	}
	conn, err := db.OpenWithPool(ctx, cfg.Database.URL, pool)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := db.RunSystem(ctx, conn, commandToken, fn); err != nil {
		return fmt.Errorf("app: command: %w", err)
	}
	return nil
}
