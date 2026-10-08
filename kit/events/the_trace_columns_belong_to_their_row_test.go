package events_test

// Three new columns, one question nobody asked.
//
// migrations/000027 put traceparent and tracestate on platformkit_outbox and
// migrations/000029 put baggage beside them; modules/audit's 000028 put trace_id
// on audit_events. The claim these files make is that the row is the right place
// to keep a publisher's context — and the row is a tenant's row, protected by the
// policy migrations/000001 describes. Two tests already cover the surrounding
// ground: migrations/rls_test.go proves every table (these two among them) carries
// a tenant policy, and kit/db proves the mechanism binds an application role.
// Neither reads *these* columns back through a second tenant's transaction, which
// is the only thing a new column on a tenant-scoped table can break: a policy
// covers rows, so a column that escaped the policy would have to be reached some
// other way — a view, a function, a cast, a grant — and none of those is checked
// by "the table has a policy".
//
// The publisher's trace context is not itself a secret, but it is a tenant's
// correlation identifier: the request id in the baggage member is the same string
// that tenant saw in its own X-Request-ID header and is quoted in its own tickets.
// Reading it from another tenant's transaction is a leak of that tenant's traffic
// pattern, and updating it is worse — it lets one tenant rewrite the join another
// tenant's audit trail relies on.
//
// So: tenant A publishes from inside a span; tenant B, in its own transaction over
// the same table, must see no row, no traceparent and no baggage, and must move
// nothing when it tries to overwrite them. Tenant A sees its own row and its own
// context. Each count is read from the database itself, so the case cannot pass by
// reading a Go value that never went near the table.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// round3Other is a second customer, sharing this schema and this table.
var round3Other = tenancy.Tenant{ID: uuid.New(), Slug: "other", Name: "Other"}

// TestATenantSeesNoTraceContextThatIsNotItsOwn reads the three columns this branch
// added, from the tenant that owns the row and from one that does not.
func TestATenantSeesNoTraceContextThatIsNotItsOwn(t *testing.T) {
	otel.SetTextMapPropagator(telemetry.Propagators())
	_, conn := dbtest.Schema(t)

	publishInsideTrace(t, conn, "billing.invoice_issued")

	// The owner sees its own row and the context it was written inside, so the
	// queries below are known to reach a row that exists: a case that only counts
	// zeros passes on an empty table.
	type counts struct {
		rows, traced, correlated int
		parent                   string
	}
	var owner counts
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*), count(traceparent), count(baggage),
			COALESCE(max(traceparent), '') FROM platformkit_outbox`).Row().Scan(
			&owner.rows, &owner.traced, &owner.correlated, &owner.parent)
	})
	if err != nil {
		t.Fatalf("the owner's read: %v", err)
	}
	if owner.rows != 1 {
		t.Fatalf("tenant acme sees %d outbox rows after publishing one, want 1", owner.rows)
	}
	if want := publisherTraceparent(); owner.parent != want {
		t.Fatalf("tenant acme reads traceparent %q, want the publisher's %q", owner.parent, want)
	}

	// The other tenant: no row, so no traceparent and no baggage either — a policy
	// that hid the row but exposed the column would show up here.
	var guest counts
	err = db.Run(tenancy.WithTenant(t.Context(), round3Other), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*), count(traceparent), count(baggage),
			COALESCE(max(traceparent), '') FROM platformkit_outbox`).Row().Scan(
			&guest.rows, &guest.traced, &guest.correlated, &guest.parent)
	})
	if err != nil {
		t.Fatalf("the second tenant's read: %v", err)
	}
	if guest.rows != 0 || guest.traced != 0 || guest.correlated != 0 || guest.parent != "" {
		t.Errorf("tenant %q reads %d outbox rows (%d with a traceparent, %d with a baggage, max=%q); "+
			"row-level security shows it nothing: a publisher's trace context and request id are the "+
			"other tenant's correlation identifiers",
			round3Other.Slug, guest.rows, guest.traced, guest.correlated, guest.parent)
	}

	// And a write from the second tenant reaches no row: RLS applies its USING
	// clause to UPDATE, so the statement succeeds and changes nothing. If it ever
	// changed one, a tenant could rewrite the trace another tenant's audit trail
	// joins on.
	var touched int64
	err = db.Run(tenancy.WithTenant(t.Context(), round3Other), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		res := tx.DB().Exec(`UPDATE platformkit_outbox SET baggage = 'pkit.request.id=forged',
			traceparent = '00-00000000000000000000000000000000-0000000000000000-01'`)
		if res.Error != nil {
			return res.Error
		}
		touched = res.RowsAffected
		return nil
	})
	if err != nil {
		// Refused outright — no UPDATE privilege, or a policy that errors. Either
		// answer is a refusal, and this case is about nothing reaching the row.
		t.Logf("the second tenant's UPDATE was refused: %v", err)
	}
	if touched != 0 {
		t.Errorf("tenant %q changed %d of tenant acme's outbox rows, want 0", round3Other.Slug, touched)
	}

	// The owner's row still carries what its own request wrote — the refusal above
	// is a refusal, and not the row having been quietly rewritten by somebody else.
	var after counts
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*), count(traceparent), count(baggage),
			COALESCE(max(traceparent), '') FROM platformkit_outbox`).Row().Scan(
			&after.rows, &after.traced, &after.correlated, &after.parent)
	})
	if err != nil {
		t.Fatalf("the owner's read back: %v", err)
	}
	if after.parent != owner.parent {
		t.Errorf("after the second tenant's UPDATE the owner's row carries traceparent %q, want %q",
			after.parent, owner.parent)
	}
}
