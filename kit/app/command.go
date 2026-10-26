package app

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"time"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/tenancy"
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
// minted under kit/internal/syscap, internal precisely so that no module and no
// composition can open a cross-tenant transaction by asking for one. What a
// command needs the door for is resolving a tenant by its slug — a control-plane
// read — and handing that tenant to db.InTenant, which lends a tenant view of the
// same transaction to the work: the widening stops at InTenant, so a command
// never gets a way to write another tenant's rows. It does not migrate either; a
// command runs against the installation the operator already has, and a schema
// this binary expects but the database does not is `platformkit migrate`'s answer.
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
	state := &commandAdmission{conn: conn}
	ctx = context.WithValue(ctx, commandAdmissionKey{}, state)
	refused := false
	defer func() {
		if v := recover(); v != nil {
			if state.ctx != nil {
				_ = events.FailOperation(state.ctx, conn, state.clock())
			}
			panic(v)
		}
	}()
	err = db.RunSystem(ctx, conn, commandToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		if err := fn(ctx, tx); err != nil {
			refused = true
			return err
		}
		if state.ctx != nil {
			if err := events.SucceedFor(state.ctx, tx, state.clock()); err != nil {
				refused = true
				return err
			}
		}
		return nil
	})
	if err != nil && refused && state.ctx != nil {
		if evidence := events.FailOperation(state.ctx, conn, state.clock()); evidence != nil {
			return fmt.Errorf("app: command failure evidence: %w", evidence)
		}
	}
	if err != nil {
		return fmt.Errorf("app: command: %w", err)
	}
	return nil
}

type commandAdmissionKey struct{}
type commandAdmission struct {
	conn  *db.Conn
	ctx   context.Context
	clock func() time.Time
}

// AdmitCommand binds one tenant command run by RunCommand after the operator
// and target actor have been checked. Seed impersonation grants no person
// projection. The returned causality commits with the existing system owner.
func AdmitCommand(ctx context.Context, tx db.Tx[db.Tenant], name string, clock func() time.Time) (context.Context, error) {
	state, ok := ctx.Value(commandAdmissionKey{}).(*commandAdmission)
	if !ok || state.ctx != nil {
		return nil, fmt.Errorf("app: command admission is unavailable or already bound")
	}
	if clock == nil {
		clock = db.Now
	}
	p, _ := tenancy.PrincipalFrom(ctx)
	admitted, err := events.AdmitCommand(ctx, state.conn, idempotency.Scope{Tenant: db.TenantOf(tx).ID, Caller: p.UserID, Route: name}, uuid.Nil, clock)
	if err != nil {
		return nil, err
	}
	state.ctx, state.clock = admitted, clock
	return admitted, nil
}
