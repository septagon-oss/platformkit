package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

var ErrAmbientTransaction = errors.New("db: an enclosing transaction already owns this operation")

// RunOwned opens and finishes its own transaction for the explicit tenant.
// Unlike Run, it refuses an existing transaction or pending request, including
// one that has not queried yet. Use the caller's Tx for composed operations.
// A commit error is not proof of rollback: the connection may fail after commit.
func RunOwned(ctx context.Context, c *Conn, tenant tenancy.Tenant, fn func(context.Context, Tx[Tenant]) error) error {
	if _, ok := current(ctx); ok {
		return ErrAmbientTransaction
	}
	if _, ok := pendingOf(ctx); ok {
		return ErrAmbientTransaction
	}
	if tenant.ID == uuid.Nil {
		return ErrNoTenant
	}
	if c == nil || fn == nil {
		return fmt.Errorf("db: connection and callback are required")
	}
	ctx = tenancy.WithTenant(ctx, tenant)
	p := &Pending{conn: c, tenant: tenant}
	tx, err := p.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			_ = p.Close(false)
			panic(r)
		}
	}()
	owned := context.WithValue(ctx, txKey{}, openTx{db: tx.db, tenant: tenant})
	if err := fn(owned, tx); err != nil {
		_ = p.Close(false)
		return err
	}
	return p.Close(true)
}
