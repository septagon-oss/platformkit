package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

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

	// ErrNoSystemToken is returned by RunSystem for a nil token. Only
	// kit/internal/syscap can produce one, so a nil token is the only forgery
	// Go allows and this is where it stops.
	ErrNoSystemToken = errors.New("db: system token was not minted by the kernel")

	// ErrScopeTampered is returned when a transaction ends under different
	// settings than the ones it opened with. See sealed.
	ErrScopeTampered = errors.New("db: the transaction's tenancy settings were rewritten inside it")
)

// Scope is what a transaction may see. The set is closed: one tenant, or every
// tenant. isScope is unexported so no package outside kit/db can add a third.
type Scope interface{ isScope() }

// Tenant scopes a transaction to the tenant that opened it.
type Tenant struct{ tenant tenancy.Tenant }

// System scopes a transaction across every tenant. It carries nothing: the
// reason a system transaction was opened belongs to the token and to the log
// line RunSystem writes, not to the handle.
type System struct{}

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
// transaction, under the settings Run, RunSystem or Pending placed.
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
	if t, ok := ctx.Value(txKey{}).(openTx); ok {
		return t, true
	}
	// A request's transaction counts from the moment it is actually open, and
	// not before: that is what lets a liveness probe reach a tenant host, and a
	// readiness check open a system transaction, while the request holds a
	// Pending it never used.
	if p, ok := pendingOf(ctx); ok {
		if gtx := p.handle(); gtx != nil {
			return openTx{db: gtx, tenant: p.tenant}, true
		}
	}
	return openTx{}, false
}

// Detached returns ctx with no transaction on it, so that a Run or a RunSystem
// below it opens its own instead of joining — or refusing to join — the one the
// caller already holds.
//
// It exists for one shape of work: a request that has resolved a tenant, opened
// a tenant transaction to recognise its caller, and now has to touch a table
// that belongs to no tenant. Those are two transactions and they cannot be one,
// because a system transaction cannot widen a tenant one half way through.
//
// The consequence is the thing to understand, and it is why this is spelled out
// rather than hidden: the detached transaction commits on its own, so its work
// survives a request that afterwards fails. A caller uses it for a control-plane
// write it means to keep — creating a tenant, recording a failed login — and for
// nothing else.
func Detached(ctx context.Context) context.Context {
	return context.WithValue(context.WithValue(ctx, txKey{}, nil), pendingKey{}, nil)
}

// Run opens a tenant-scoped transaction for the tenant in ctx and applies it
// with set_config('platformkit.tenant_id', ..., true), which is what the
// row-level security policies in migrations/ read. Nested calls join the
// enclosing tenant transaction, including a request's Pending one.
func Run(ctx context.Context, c *Conn, fn func(ctx context.Context, tx Tx[Tenant]) error) error {
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
	if p, ok := pendingOf(ctx); ok {
		// A request that has not queried yet. Opening here hands the caller the
		// request's own transaction; the request, not this call, ends it.
		if p.tenant.ID != t.ID {
			return ErrScopeMismatch
		}
		tx, err := p.Tx(ctx)
		if err != nil {
			return err
		}
		return fn(ctx, tx)
	}

	p := &Pending{conn: c, tenant: t}
	tx, err := p.Tx(ctx)
	if err != nil {
		return err
	}
	// A panic must not leave the transaction open on the pooled connection.
	defer func() {
		if r := recover(); r != nil {
			_ = p.Close(false)
			panic(r)
		}
	}()
	if err := fn(context.WithValue(ctx, txKey{}, openTx{db: tx.db, tenant: t}), tx); err != nil {
		_ = p.Close(false)
		return err
	}
	return p.Close(true)
}

// RunSystem opens a cross-tenant transaction. Only packages under kit/ can
// obtain a token, and every open is logged with the reason it was minted for.
// Nested calls join an enclosing system transaction; an open tenant transaction
// is a scope mismatch, because crossing tenants half way through tenant work is
// never what the caller meant.
func RunSystem(ctx context.Context, c *Conn, tok tenancy.SystemToken, fn func(ctx context.Context, tx Tx[System]) error) error {
	if tok == nil {
		return ErrNoSystemToken
	}
	if cur, ok := current(ctx); ok {
		if !cur.system {
			return ErrScopeMismatch
		}
		return fn(ctx, Tx[System]{db: cur.db})
	}
	// Debug, not Info: a cross-tenant transaction is rare by design, but the
	// readiness probe and every host resolution open one, and a log line per
	// probe is a log nobody reads.
	slog.DebugContext(ctx, "db: cross-tenant transaction", "reason", tok.Reason())
	return c.db.WithContext(ctx).Transaction(func(gtx *gorm.DB) error {
		if err := gtx.Exec("SELECT set_config('platformkit.system_access', 'true', true)").Error; err != nil {
			return fmt.Errorf("db: set system access: %w", err)
		}
		inner := context.WithValue(ctx, txKey{}, openTx{db: gtx, system: true})
		if err := fn(inner, Tx[System]{db: gtx}); err != nil {
			return err
		}
		return sealed(gtx, "", "true")
	})
}

// Pending is a tenant transaction that has not been opened yet.
//
// It exists for one caller, kit/httpx: a request cannot know at its start
// whether it will query, and opening a transaction for one that never does
// means a liveness probe sent to a tenant host fails while the database is
// down — which restarts every replica during the outage instead of after it.
// So the request carries a Pending, the first Tx or Run opens it, and Close
// commits or rolls back once the response is decided.
type Pending struct {
	conn   *Conn
	tenant tenancy.Tenant

	mu  sync.Mutex
	gtx *gorm.DB
	err error
}

type pendingKey struct{}

func pendingOf(ctx context.Context) (*Pending, bool) {
	p, ok := ctx.Value(pendingKey{}).(*Pending)
	return p, ok
}

// Lazy puts a Pending for the tenant in ctx on the returned context. Nothing
// reaches the database until something asks for the transaction.
//
// It takes a kernel token for the same reason RunSystem does. A Pending is a
// transaction whose commit is somebody else's decision, which is a capability
// no module may hold: Run opens and closes its own transaction in one call,
// and that is the only door outside kit/. Only kit/httpx mints a token for it.
func Lazy(ctx context.Context, c *Conn, tok tenancy.SystemToken) (context.Context, *Pending, error) {
	if tok == nil {
		return ctx, nil, ErrNoSystemToken
	}
	t, ok := tenancy.FromContext(ctx)
	if !ok {
		return ctx, nil, ErrNoTenant
	}
	p := &Pending{conn: c, tenant: t}
	return context.WithValue(ctx, pendingKey{}, p), p, nil
}

// Tx opens the transaction if it is not open yet and returns the handle. A
// failed open is remembered: a request that could not reach the database asks
// once, not once per query.
func (p *Pending) Tx(ctx context.Context) (Tx[Tenant], error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return Tx[Tenant]{}, p.err
	}
	if p.gtx == nil {
		gtx := p.conn.db.WithContext(ctx).Begin()
		if gtx.Error != nil {
			p.err = fmt.Errorf("db: begin: %w", gtx.Error)
			return Tx[Tenant]{}, p.err
		}
		if err := gtx.Exec("SELECT set_config('platformkit.tenant_id', ?, true)", p.tenant.ID.String()).Error; err != nil {
			_ = gtx.Rollback().Error
			p.err = fmt.Errorf("db: set tenant: %w", err)
			return Tx[Tenant]{}, p.err
		}
		p.gtx = gtx
	}
	return Tx[Tenant]{db: p.gtx, scope: Tenant{tenant: p.tenant}}, nil
}

// Err is the failure that stopped the transaction opening, if one did.
func (p *Pending) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *Pending) handle() *gorm.DB {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gtx
}

// Close ends the transaction: it commits when keep is true and the settings the
// open placed are still the ones in force, and rolls back otherwise. It is a
// no-op when nothing was ever opened, and idempotent.
func (p *Pending) Close(keep bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	gtx := p.gtx
	p.gtx = nil
	if gtx == nil {
		return nil
	}
	if !keep {
		return gtx.Rollback().Error
	}
	if err := sealed(gtx, p.tenant.ID.String(), ""); err != nil {
		_ = gtx.Rollback().Error
		return err
	}
	return gtx.Commit().Error
}

// sealed refuses to commit a transaction that ends under different settings
// than the ones its runner placed.
//
// This is a backstop, and the precise claim matters. platformkit.tenant_id and
// platformkit.system_access are placeholder GUCs, which are USERSET: any
// statement inside the transaction can rewrite them, and no database privilege
// prevents it. This check catches the naive escape — code that sets a setting
// and leaves it set — and it catches nothing else: an escape that restores the
// value before returning re-reads clean and commits. There is no way to close
// that with a re-read, because the state it inspects is the state the escape
// restored.
//
// So the control for a deliberate escape is not here. It is scripts/check_gucs.sh,
// which fails the build when any .go file outside kit/db writes either setting,
// and the type parameter, which makes crossing the tenant by accident
// impossible. This costs one round trip per transaction, both settings in one
// query, and it is worth that because the naive escape is the one that happens.
func sealed(gtx *gorm.DB, wantTenant, wantSystem string) error {
	var tenant, system string
	const q = `SELECT coalesce(current_setting('platformkit.tenant_id', true), ''),
		coalesce(current_setting('platformkit.system_access', true), '')`
	if err := gtx.Raw(q).Row().Scan(&tenant, &system); err != nil {
		return fmt.Errorf("db: re-read the transaction settings: %w", err)
	}
	if tenant != wantTenant || system != wantSystem {
		return fmt.Errorf("%w: it ends with tenant_id=%q system_access=%q, having opened with %q and %q",
			ErrScopeTampered, tenant, system, wantTenant, wantSystem)
	}
	return nil
}
