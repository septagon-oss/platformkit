package limit_test

// queued_lock_budget_test.go pins the two properties of the queued row that the
// burst cases do not: the wait is ended by the server's own lock budget (SQLSTATE
// 55P03, README "the two budgets"), not by the two-second wall, and a queue at one
// tenant's key refuses that tenant only.

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestAQueuedAttemptIsEndedByTheServersLockBudget: with every lock_timeout line
// removed from limit.go, each other case in this package still passes, because
// the wall turns the same wait into context.DeadlineExceeded. This one reads the
// world the README says the package learns it from — the driver's 55P03 —
// through the ErrBusy that Forget answers, which wraps it.
func TestAQueuedAttemptIsEndedByTheServersLockBudget(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	l := limit.Postgres(httpx.ConnFrom)
	if _, _, err := l.Allow(ctx, "ada", 3, window); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	dbtest.Hold(t, admin, 10*time.Second,
		"UPDATE platformkit_limits SET count = count WHERE key = $1", storedKeys(t, admin, 1)[0])

	err := l.Forget(ctx, "ada")
	if !errors.Is(err, limit.ErrBusy) {
		t.Fatalf("Forget behind the row's lock = %v; want ErrBusy", err)
	}
	pg, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || pg.Code != "55P03" {
		t.Errorf("Forget behind the row's lock ended with %v; want the server's lock_timeout (SQLSTATE 55P03) "+
			"rather than the wall", err)
	}
}

// TestAQueueAtOneTenantsKeyDoesNotRefuseAnother is tenant isolation under the
// cure: the key is scoped by tenant, so another tenant's attempt at the same
// name is a different row, never queues, and is counted.
func TestAQueueAtOneTenantsKeyDoesNotRefuseAnother(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	acmeCtx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	globexCtx := httpx.WithConn(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New(), Slug: "globex"}), conn)
	l := limit.Postgres(httpx.ConnFrom)
	if _, _, err := l.Allow(acmeCtx, "ada", 3, window); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	dbtest.Hold(t, admin, 10*time.Second,
		"UPDATE platformkit_limits SET count = count WHERE key = $1", storedKeys(t, admin, 1)[0])

	if ok, retry, err := l.Allow(acmeCtx, "ada", 3, window); err != nil || ok || retry != window {
		t.Errorf("acme's queued attempt = %v, %s, %v; want refused with the whole window and no error", ok, retry, err)
	}
	if ok, _, err := l.Allow(globexCtx, "ada", 3, window); err != nil || !ok {
		t.Errorf("globex's attempt at the same name while acme's row is held = %v, %v; want counted and allowed", ok, err)
	}
	if n, _, err := l.Count(globexCtx, "ada", window); err != nil || n != 1 {
		t.Errorf("globex's count = %d, %v; want its own 1", n, err)
	}
	if n, _, err := l.Count(acmeCtx, "ada", window); err != nil || n != 1 {
		t.Errorf("acme's count = %d, %v; want the 1 it had before the queue, untouched by globex", n, err)
	}
}
