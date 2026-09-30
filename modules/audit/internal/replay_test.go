package internal_test

// The trail's idempotence key is (tenant_id, event_id) — migrations/000015 added the
// tenant to it and dropped the global unique index of migrations/000010. Nothing read
// that key until now. T-0115 is the first code to depend on it for a design decision:
// `modules/tenant` writes one lifecycle verb as two events in two tenants — the customer's
// own row and the installation's mirror of it — and says in its own comment why the
// duplicate it wants stopped is stopped by the command and not by the table ("the trail's
// idempotence key is (tenant_id, event_id) and these are two event ids"). Two rows per
// verb is a decision that stands only while a redelivery cannot write a third row and one
// tenant's row cannot suppress another tenant's. Both halves are pinned here, at the trail,
// which is where the key lives.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

// trailTotal reads one tenant's trail as the screen reads it, through the read door, and
// returns how many rows it holds. The count and not the page: what a replay can change is
// the count.
func trailTotal(t *testing.T, conn *db.Conn, svc *internal.Service, tenant tenancy.Tenant) int64 {
	t.Helper()
	var total int64
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, n, err := svc.List(ctx, tx, contracts.Query{})
		total = n
		return err
	})
	if err != nil {
		t.Fatalf("read %s's trail: %v", tenant.Slug, err)
	}
	return total
}

// TestARedeliveredEventLeavesOneTrailRow is the second lock behind the kernel's handled
// table: a delivery that claims an event id is one row, and a delivery that does not —
// an operator replaying an outbox row, or a handler that failed after it wrote — is none.
func TestARedeliveredEventLeavesOneTrailRow(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()

	ev := events.Event{
		ID: uuid.New(), TenantID: acme.ID, Name: "task.task.created", At: db.Now(),
		TraceParent: trace.New().Parent(), Payload: []byte(`{"title":"chiller-2"}`),
	}
	// Each recording is its own transaction, which is the shape of a redelivery: the
	// first committed, and this key is the only thing between that commit and a second
	// row. Neither call may error — DO NOTHING answers success, not a conflict — and
	// neither may add a row.
	for delivery := 1; delivery <= 2; delivery++ {
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return svc.Record(ctx, tx, ev)
		})
		if err != nil {
			t.Fatalf("record delivery %d of %s: %v", delivery, ev.Name, err)
		}
	}
	if got := trailTotal(t, conn, svc, acme); got != 1 {
		t.Fatalf("one event id delivered twice left %d rows in acme's trail, want the one it wrote first", got)
	}

	// The tenant half of the key. The same id handed to another tenant's transaction is a
	// row in that tenant's trail: under the global index of migrations/000010 the second
	// insert would be swallowed by the first tenant's row, and two tenants that arrived at
	// one event id — a restore, an import, a replay across tenants — would silently lose
	// one trail row.
	if err := db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Record(ctx, tx, ev)
	}); err != nil {
		t.Fatalf("record the same event id in globex: %v", err)
	}
	if got := trailTotal(t, conn, svc, globex); got != 1 {
		t.Errorf("the same event id recorded in a second tenant left %d rows there, want its own", got)
	}
	if got := trailTotal(t, conn, svc, acme); got != 1 {
		t.Errorf("globex's row changed acme's trail to %d rows; the key that is shared across tenants suppresses one", got)
	}
}
