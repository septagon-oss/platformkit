package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

var (
	// ErrNoTenant is returned by Run when the context carries no tenant.
	ErrNoTenant = errors.New("db: no tenant in context")

	// ErrScopeMismatch is returned when a transaction cannot join the one
	// already open on the context: a system transaction inside a tenant one, a
	// tenant transaction inside a system one, or a second tenant inside the
	// first tenant's transaction.
	ErrScopeMismatch = errors.New("db: transaction scope mismatch")

	// ErrNoSystemToken is returned by RunSystem for a token kit/internal/syscap
	// did not mint. Go lets any package write tenancy.SystemToken{}, so the
	// compile-time barrier (only kit/ may import syscap) needs this runtime
	// backstop: a forged token carries no reason, and a reason is mandatory.
	ErrNoSystemToken = errors.New("db: system token was not minted by the kernel")

	// errOwnerConn keeps application code off the DDL connection, which owns
	// the tables and is therefore the one connection policies would not bind.
	errOwnerConn = errors.New("db: the owner connection is exempt from row-level security; open the application connection with Open")
)

// Scope is what a transaction may see. The set is closed: one tenant, or every
// tenant. isScope is unexported so no package outside kit/db can add a third.
type Scope interface{ isScope() }

// Tenant scopes a transaction to the tenant that opened it.
type Tenant struct{ tenant tenancy.Tenant }

// System scopes a transaction across every tenant.
type System struct{ reason string }

func (Tenant) isScope() {}
func (System) isScope() {}

// Tx is a transaction-bound handle whose scope is part of its type. A
// repository that takes Tx[Tenant] cannot be handed a Tx[System], and neither
// can be constructed outside this package, so there is no way to query outside
// a transaction. See scope_compile_test.go.
type Tx[S Scope] struct {
	db    *gorm.DB
	scope S
}

// DB is the transaction-bound GORM handle. Every query on it runs inside the
// transaction, under the settings Run or RunSystem placed.
func (t Tx[S]) DB() *gorm.DB { return t.db }

// TenantOf is the tenant a tenant-scoped transaction belongs to. It is a
// function rather than a method because Go does not allow a method on one
// instantiation of a generic type.
func TenantOf(tx Tx[Tenant]) tenancy.Tenant { return tx.scope.tenant }

// openTx is the transaction already running on a context. Nesting joins it
// rather than opening a second one, so a handler that calls two services still
// commits once.
type openTx struct {
	db     *gorm.DB
	system bool
	tenant tenancy.Tenant
}

type txKey struct{}

func current(ctx context.Context) (openTx, bool) {
	t, ok := ctx.Value(txKey{}).(openTx)
	return t, ok
}

// Run opens a tenant-scoped transaction for the tenant in ctx and applies it
// with set_config('platformkit.tenant_id', ..., true), which is what the
// row-level security policies in migrations/ read. Nested calls join the
// enclosing tenant transaction.
func Run(ctx context.Context, c *Conn, fn func(ctx context.Context, tx Tx[Tenant]) error) error {
	if c.owner {
		return errOwnerConn
	}
	t, ok := tenancy.FromContext(ctx)
	if !ok {
		return ErrNoTenant
	}
	if cur, ok := current(ctx); ok {
		if cur.system || cur.tenant.ID != t.ID {
			return ErrScopeMismatch
		}
		return fn(ctx, Tx[Tenant]{db: cur.db, scope: Tenant{tenant: t}})
	}
	return c.db.WithContext(ctx).Transaction(func(gtx *gorm.DB) error {
		if err := gtx.Exec("SELECT set_config('platformkit.tenant_id', ?, true)", t.ID.String()).Error; err != nil {
			return fmt.Errorf("db: set tenant: %w", err)
		}
		inner := context.WithValue(ctx, txKey{}, openTx{db: gtx, tenant: t})
		return fn(inner, Tx[Tenant]{db: gtx, scope: Tenant{tenant: t}})
	})
}

// RunSystem opens a cross-tenant transaction. Only packages under kit/ can
// obtain a token, and every open is logged with the reason it was minted for.
// Nested calls join an enclosing system transaction; a tenant transaction is a
// scope mismatch, because crossing tenants half way through tenant work is
// never what the caller meant.
func RunSystem(ctx context.Context, c *Conn, tok tenancy.SystemToken, fn func(ctx context.Context, tx Tx[System]) error) error {
	if c.owner {
		return errOwnerConn
	}
	reason := tok.Reason()
	if reason == "" {
		return ErrNoSystemToken
	}
	if cur, ok := current(ctx); ok {
		if !cur.system {
			return ErrScopeMismatch
		}
		return fn(ctx, Tx[System]{db: cur.db, scope: System{reason: reason}})
	}
	slog.InfoContext(ctx, "db: cross-tenant transaction", "reason", reason)
	return c.db.WithContext(ctx).Transaction(func(gtx *gorm.DB) error {
		if err := gtx.Exec("SELECT set_config('platformkit.system_access', 'true', true)").Error; err != nil {
			return fmt.Errorf("db: set system access: %w", err)
		}
		inner := context.WithValue(ctx, txKey{}, openTx{db: gtx, system: true})
		return fn(inner, Tx[System]{db: gtx, scope: System{reason: reason}})
	})
}
