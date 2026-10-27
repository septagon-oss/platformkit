package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/telemetry"
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
// a transaction.
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
	span   trace.Span
	view   *tenantView
	// commits is the work the transaction that will end this one runs after it
	// commits and rollbacks the work it runs after a rollback it confirmed. Both are
	// nil for a system transaction, which defers neither: ErrNoTransactionToDefer.
	commits   *afterQueue
	rollbacks *afterQueue
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
		if gtx, span, commits, rollbacks := p.frame(); gtx != nil {
			return openTx{db: gtx, tenant: p.tenant, span: span, commits: commits, rollbacks: rollbacks}, true
		}
	}
	return openTx{}, false
}

// TxFrom returns the tenant transaction this context is already inside — the one Run
// applied with its own handle, or a request's transaction that has actually been
// opened — and reports false for a context that is inside no transaction, inside a
// system one, or inside a request whose transaction nobody has asked for yet.
//
// It is the read-only half of Run: a caller that has been handed a transaction by
// Run, and a layer that has to reach the database through one it was not handed, need
// the same answer, and an accessor that only knows one of the two opens a second
// transaction beside the first. A context that carries a Pending it has not opened
// reports false rather than opening it: asking for the transaction is Run's decision
// or the caller's, never a reader's. What it hands back is what Run's join hands: an
// operation of ctx, never the shared handle, so its statements carry ctx's deadline
// and values inside the transaction, and a provisioning view ends it with its
// callback. See README.md, "The transaction's owner".
func TxFrom(ctx context.Context) (Tx[Tenant], bool) {
	cur, ok := current(ctx)
	if !ok || cur.system {
		return Tx[Tenant]{}, false
	}
	if cur.view != nil {
		ctx = cur.view.bind(ctx)
	}
	return Tx[Tenant]{db: operation(cur.db, cur.span, ctx), scope: Tenant{tenant: cur.tenant}}, true
}

// HasTransaction reports both active and lazy transactions carried by ctx.
// A background owner uses it to refuse accidentally sharing a caller's commit.
func HasTransaction(ctx context.Context) bool {
	if _, ok := pendingOf(ctx); ok {
		return true
	}
	_, ok := current(ctx)
	return ok
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
			// A nested frame that cannot join still has to say so before it defers
			// anything of its own; AfterCommit reads the frame below, not this one.
			return ErrScopeMismatch
		}
		if cur.view != nil {
			ctx = cur.view.bind(ctx)
		}
		return fn(ctx, Tx[Tenant]{db: operation(cur.db, cur.span, ctx), scope: Tenant{tenant: t}})
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

	p := newPending(c, t, ctx)
	tx, err := p.Tx(ctx)
	if err != nil {
		return err
	}
	// A panic must not leave the transaction open on the pooled connection.
	defer func() {
		if r := recover(); r != nil {
			_ = p.Rollback()
			panic(r)
		}
	}()
	// A nested join clones the shared handle, never this caller's operation handle.
	gtx, span, commits, rollbacks := p.frame()
	if err := fn(context.WithValue(ctx, txKey{}, openTx{db: gtx, tenant: t, span: span, commits: commits, rollbacks: rollbacks}), tx); err != nil {
		_ = p.Rollback()
		return err
	}
	return p.Commit()
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
	ctx, span := telemetry.Tracer().Start(ctx, "cross-tenant transaction",
		trace.WithAttributes(append(telemetry.SpanAttrs(ctx),
			attribute.String("pkit.system.reason", tok.Reason()))...))
	defer func() { span.End() }()
	err := c.db.WithContext(ctx).Transaction(func(gtx *gorm.DB) error {
		if err := gtx.Exec("SELECT set_config('platformkit.system_access', 'true', true)").Error; err != nil {
			return fmt.Errorf("db: set system access: %w", err)
		}
		inner := context.WithValue(ctx, txKey{}, openTx{db: gtx, system: true})
		if err := fn(inner, Tx[System]{db: gtx}); err != nil {
			return err
		}
		return sealed(gtx, "", "true")
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

// Pending is a tenant transaction that has not been opened yet.
//
// It exists for one caller, kit/httpx: a request cannot know at its start
// whether it will query, and opening a transaction for one that never does
// means a liveness probe sent to a tenant host fails while the database is
// down — which restarts every replica during the outage instead of after it.
// So the request carries a Pending, the first Tx or Run opens it, and Close
// commits or rolls back once the response is decided.
//
// The transaction belongs to its owner, the context the Pending was created
// with: BEGIN, the span, the settings re-read and COMMIT run under it, and once
// it ends nothing commits and nothing opens. Each caller of Tx gets a handle
// bound to its own context, so its deadline ends its statements and not the
// transaction. See README.md, "The transaction's owner".
type Pending struct {
	conn   *Conn
	tenant tenancy.Tenant
	owner  context.Context

	mu   sync.Mutex
	gtx  *gorm.DB
	err  error
	span trace.Span
	// commits and rollbacks are pointers, and not for taste: Pending is compared by
	// callers of the API this package publishes, and a queue holding a slice makes it
	// not comparable, which the exported-API gate refuses as a new incompatibility.
	commits   *afterQueue
	rollbacks *afterQueue
}

// newPending is the only way to hold one. The queues that wait for an outcome come with
// the transaction that can have one, in open, and not here: a Pending whose transaction
// has ended owns no queue, which is what keeps an effect registered to one transaction
// out of the next one's ending.
func newPending(c *Conn, t tenancy.Tenant, owner context.Context) *Pending {
	return &Pending{conn: c, tenant: t, owner: owner}
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
	p := newPending(c, t, ctx)
	return context.WithValue(ctx, pendingKey{}, p), p, nil
}

// Tx opens the transaction if it is not open yet and returns a handle whose
// statements run under ctx, the caller's operation context, inside the owner's
// transaction. A failed open under a live owner is remembered: a request that
// could not reach the database asks once, not once per query. An owner that has
// ended refuses every open; a caller that has ended is refused alone, and the
// next live caller may still open.
func (p *Pending) Tx(ctx context.Context) (Tx[Tenant], error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return Tx[Tenant]{}, p.err
	}
	if p.gtx == nil {
		if p.owner.Err() != nil {
			return Tx[Tenant]{}, fmt.Errorf("db: the transaction's owner ended before it opened: %w", context.Cause(p.owner))
		}
		if ctx.Err() != nil {
			return Tx[Tenant]{}, fmt.Errorf("db: the caller ended before the transaction opened: %w", context.Cause(ctx))
		}
		if err := p.open(); err != nil {
			return Tx[Tenant]{}, err
		}
	}
	return Tx[Tenant]{db: operation(p.gtx, p.span, ctx), scope: Tenant{tenant: p.tenant}}, nil
}

// open begins the transaction under the owner and places the tenant. A failure
// caused by the owner ending (a cancel while waiting for a connection) is
// returned and not remembered, so Err keeps meaning the database refused.
func (p *Pending) open() error {
	// The span opens with the transaction and not with the Pending: a request
	// that never queries opens neither, which is the whole reason Pending
	// exists. One span per opened transaction, under whatever span the work already
	// sits in, is what makes "SQL under Tx" a boundary a reader can see: its
	// duration is the transaction's, from BEGIN to the commit that was checked.
	// Not one span per statement — kit/db's promise is that GORM is the SQL
	// executor and nothing else, and a per-statement span is bought with a
	// callback; "which statement was the slow one" is the statement log's answer.
	ctx, span := telemetry.Tracer().Start(p.owner, "database transaction",
		trace.WithAttributes(telemetry.SpanAttrs(p.owner)...))
	// One pair of queues per opened transaction, for as long as that transaction is
	// open: see Close. A Pending that reopened after a rollback gets queues that owe
	// nothing to the transaction that ended.
	p.commits, p.rollbacks = &afterQueue{}, &afterQueue{}
	gtx := p.conn.db.WithContext(ctx).Begin()
	if gtx.Error != nil {
		p.commits, p.rollbacks = nil, nil
		return p.failed(span, fmt.Errorf("db: begin: %w", gtx.Error))
	}
	if err := gtx.Exec("SELECT set_config('platformkit.tenant_id', ?, true)", p.tenant.ID.String()).Error; err != nil {
		_ = gtx.Rollback().Error
		p.commits, p.rollbacks = nil, nil
		return p.failed(span, fmt.Errorf("db: set tenant: %w", err))
	}
	p.gtx, p.span = gtx, span
	return nil
}

// failed ends the span of an open that did not happen and remembers err unless
// the owner ending is what stopped it.
func (p *Pending) failed(span trace.Span, err error) error {
	if p.owner.Err() != nil {
		err = fmt.Errorf("db: the transaction's owner ended before it opened: %w (%w)", context.Cause(p.owner), err)
	} else {
		p.err = err
	}
	endErr(span, err)
	return err
}

// operation is the handle one caller's statements run through: a clone of the
// shared transaction handle carrying the caller's deadline and values, with the
// transaction's span as its current span. The shared handle is never changed.
func operation(gtx *gorm.DB, span trace.Span, ctx context.Context) *gorm.DB {
	if span != nil {
		ctx = trace.ContextWithSpan(ctx, span)
	}
	return gtx.WithContext(ctx)
}

// endErr is how a database failure reaches a span: the error itself, and a status,
// so a trace viewer marks the span red instead of leaving a reader to infer the
// outcome from the absence of a commit.
func endErr(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	span.End()
}

// Opened returns the transaction this Pending has opened, and false when it has
// opened none. Asking opens nothing: a request that never queried wrote nothing a
// caller could add a row beside.
func (p *Pending) Opened() (Tx[Tenant], bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gtx == nil {
		return Tx[Tenant]{}, false
	}
	return Tx[Tenant]{db: p.gtx, scope: Tenant{tenant: p.tenant}}, true
}

// Err is the failure that stopped the transaction opening, if one did.
func (p *Pending) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// frame is the open transaction, its span and the two queues belonging to it, read under
// the mutex: a reader must not hold a queue a concurrent ending has already taken.
func (p *Pending) frame() (*gorm.DB, trace.Span, *afterQueue, *afterQueue) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gtx, p.span, p.commits, p.rollbacks
}

// Commit ends the open transaction by committing it, when its owner has not ended
// and the settings its open placed are still the ones in force, and runs the
// effects registered to after the commit. It answers what Close(true) answers: an
// owner that ended first is an error wrapping its cause and nothing is committed,
// a transaction whose settings were rewritten is refused with ErrScopeTampered and
// rolled back, and a Pending with nothing open is a no-op that answers nil. It is
// idempotent, and a commit leaves the Pending unable to open again under the owner
// that ended.
func (p *Pending) Commit() error { return p.finalise(true) }

// Rollback ends the open transaction by discarding it, dropping the effects that waited
// for its commit and running the ones that waited for the rollback. It answers what
// Close(false) answers: the no-op and nil of a Pending with nothing open, idempotent, and
// — as the commit is not — able to open again under a live owner.
func (p *Pending) Rollback() error { return p.finalise(false) }

// Close ends the transaction: it commits when keep is true, the owner has not
// ended and the settings the open placed are still the ones in force, and rolls
// back otherwise. An owner that ended first is reported as an error wrapping its
// cause, whatever keep asked for. It is a no-op when nothing is open, and
// idempotent; a Close under a live owner leaves the Pending able to open again.
//
// Deprecated: Use Commit or Rollback, which say which of the two answers they ask
// for instead of a bare bool at the call site.
func (p *Pending) Close(keep bool) error {
	if keep {
		return p.Commit()
	}
	return p.Rollback()
}

// ending is what the one ending of a transaction reports: which outcome the database
// confirmed, the work that belonged to the transaction, and the database's own answer.
// committed and rollbackConfirmed are never both true, and neither is when the outcome is
// in doubt — a ROLLBACK the database refused, a COMMIT that returned an error, none open.
type ending struct {
	committed         bool
	rollbackConfirmed bool
	commits           *afterQueue
	rollbacks         *afterQueue
	err               error
}

// finalise performs the one ending Close, Commit and Rollback all ask for, and reports
// what it decided.
//
// The effects registered to an outcome belong to the transaction that registered them, so
// they run here and nowhere else: each ending runs its own work and drops the other one's.
func (p *Pending) finalise(keep bool) error {
	e := p.end(keep)
	// The queues are drained here, outside the mutex, because a queue is somebody
	// else's code and an action that opens a transaction of its own must not have to
	// wait for this Pending.
	commits, rollbacks := e.commits.drain(), e.rollbacks.drain()
	e.logUndone(p.owner, commits, rollbacks)
	if e.rollbackConfirmed {
		runRollbacks(p.conn, rollbacks)
	}
	if e.err != nil || !e.committed {
		return e.err
	}
	return runAfter(p.conn, commits)
}

// end commits or rolls back the open transaction and reports which outcome the database
// confirmed, which decides which deferred effects run and which are dropped. Both queues
// leave the Pending whatever the outcome, so nothing a later transaction registers can be
// run by this ending, and nothing this one dropped can be run by the next.
func (p *Pending) end(keep bool) ending {
	p.mu.Lock()
	defer p.mu.Unlock()
	gtx, span, commits, rollbacks := p.gtx, p.span, p.commits, p.rollbacks
	p.gtx, p.span, p.commits, p.rollbacks = nil, nil, nil, nil
	// Both queues are taken before anything else happens to them: from here the
	// transaction is over, so a registration that still reaches one — through a frame
	// its handler kept — is refused rather than dropped in silence.
	commits.take()
	rollbacks.take()
	if gtx == nil {
		return ending{}
	}
	// open sets gtx and span together, so span is live here. It ends before the
	// outcome is returned, whatever it is: the transaction is over the moment the
	// decision was made, and a rolled back one is what a reader of a failed
	// request is looking for.
	defer span.End()
	if p.owner.Err() != nil {
		_ = gtx.Rollback().Error
		err := fmt.Errorf("db: the transaction's owner ended before it was finalised: %w", context.Cause(p.owner))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		// No COMMIT was asked for on this path, and database/sql binds a transaction's life
		// to the context it began with, so these rows can never become visible whatever that
		// ROLLBACK answered — which is the outcome the rollback queue waits for.
		return ending{rollbackConfirmed: true, commits: commits, rollbacks: rollbacks, err: err}
	}
	if !keep {
		err := gtx.Rollback().Error
		return ending{rollbackConfirmed: err == nil, commits: commits, rollbacks: rollbacks, err: err}
	}
	if err := sealed(gtx, p.tenant.ID.String(), ""); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		refused := gtx.Rollback().Error
		return ending{rollbackConfirmed: refused == nil, commits: commits, rollbacks: rollbacks, err: err}
	}
	err := gtx.Commit().Error
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ending{commits: commits, rollbacks: rollbacks, err: err}
	}
	return ending{committed: true, commits: commits, rollbacks: rollbacks}
}

// logUndone writes the deferred actions this ending will not run, at the level its own answer
// decides. Every ending the database confirmed drops the queue that never waited for it, which is
// what that outcome promises and what every ordinary commit does; only the ending the database
// left without an answer loses work the caller may never hear about, and that one warns. Either
// way the actions are memory and their transaction is over, so without this line work that was
// registered and never happened leaves no trace at all. owner is the transaction's, which an
// ending may well outlive: the line is written under it, not against it.
func (e ending) logUndone(owner context.Context, commits, rollbacks []afterAction) {
	known := e.committed || e.rollbackConfirmed
	dropped := len(commits) + len(rollbacks)
	if e.committed {
		dropped = len(rollbacks)
	} else if e.rollbackConfirmed {
		dropped = len(commits)
	}
	if dropped == 0 {
		return
	}
	attrs := []any{"effects", dropped, "committed", e.committed, "rolled_back", e.rollbackConfirmed}
	if e.err != nil {
		attrs = append(attrs, "error", e.err)
	}
	level := slog.LevelWarn
	if known {
		level = slog.LevelDebug
	}
	slog.Log(context.WithoutCancel(owner), level,
		"db: the transaction ended without the effects deferred to it", attrs...)
}

// sealed refuses to commit a transaction that ends under different settings
// than the ones its runner placed.
//
// This is a backstop, and the precise claim matters: the two GUCs are USERSET, so
// any statement inside the transaction can rewrite them. This catches the naive
// escape — code that sets a setting and leaves it set — and catches nothing else,
// because an escape that restores the value before returning re-reads clean. The
// control for a deliberate escape is scripts/check_gucs.sh, which fails the build
// when anything outside kit/db writes either setting, and the type parameter, which
// makes crossing a tenant by accident impossible. It costs one round trip per
// transaction, worth it because the naive escape is the one that happens.
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
