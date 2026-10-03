package events

// The purge exemption is the row's own, one tenant wider
// than the case that first named it.
//
// `Purge` now exempts a relayed row while a dead letter still describes it:
//
// 	AND NOT EXISTS (SELECT 1 FROM platformkit_dead_letters d WHERE d.event_id = o.id)
//
// Two things about that clause are not visible in a one-tenant test.
//
// One: the sweep is a system transaction over the app role, and
// `platformkit_dead_letters` is RLS-protected like the outbox itself. A
// `NOT EXISTS` over a table whose rows the query cannot see is a `NOT EXISTS`
// that is always true, so the exemption only works because the policy answers
// `platformkit_is_system()` for this transaction. With one tenant in the
// database you cannot tell that from luck: the row survives either way. Two
// tenants, one purge, both rows still there — that is the clause reading the
// other tenant's ledger.
//
// Two: the exemption is correlated per row, not per sweep. A dead letter in one
// tenant's ledger must not shelter another tenant's row, or the purge stops
// working the day a second customer arrives. That direction only exists once
// somebody clears one dead letter and purges again: the cleared row goes, the
// one still under review stays.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestThePurgeExemptionIsTheRowsOwnAndNoOnesElse(t *testing.T) {
	fast(t)
	admin, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	acme := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	other := tenancy.Tenant{ID: uuid.New(), Slug: "othercorp"}

	transport := memory.New()
	if err := Consume(ctx, conn, transport, []Subscription{{
		Module: "ledger", Name: "ledger.invoice_issued",
		Handler: func(context.Context, db.Tx[db.Tenant], Event) error {
			return errors.New("the mailer is down")
		},
	}}); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	ids := map[uuid.UUID]uuid.UUID{}
	for _, tenant := range []tenancy.Tenant{acme, other} {
		if err := db.Run(tenancy.WithTenant(ctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return Publish(ctx, tx, "ledger.invoice_issued", map[string]string{"why": "the invoice went out"})
		}); err != nil {
			t.Fatalf("publish for %s: %v", tenant.Slug, err)
		}
	}
	if err := Relay(ctx, conn, transport); err != nil {
		t.Fatalf("relay: %v", err)
	}
	var dead int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_dead_letters`).Scan(&dead); err != nil {
		t.Fatal(err)
	}
	if dead != 2 {
		t.Fatalf("the two tenants' events did not both reach the dead-letter ledger: %d rows", dead)
	}
	// Each tenant's dead-lettered event, read by its own tenant_id: the ledger
	// rows are tenanted, and so is the outbox row behind each of them.
	for _, tenant := range []tenancy.Tenant{acme, other} {
		var id uuid.UUID
		if err := admin.QueryRowContext(ctx,
			`SELECT event_id FROM platformkit_dead_letters WHERE tenant_id=$1`, tenant.ID).Scan(&id); err != nil {
			t.Fatalf("no dead letter for %s: %v", tenant.Slug, err)
		}
		ids[tenant.ID] = id
	}

	// A week of queue latency for both rows: both are relayed, so both are
	// purgeable by the rule as written, and neither is by the rule as cured.
	for _, id := range ids {
		if _, err := admin.ExecContext(ctx,
			`UPDATE platformkit_outbox SET published_at = now() - interval '8 days' WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := Purge(ctx, conn); err != nil {
		t.Fatalf("purge: %v", err)
	}
	for _, tenant := range []tenancy.Tenant{acme, other} {
		var kept int
		if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_outbox WHERE id=$1`,
			ids[tenant.ID]).Scan(&kept); err != nil {
			t.Fatal(err)
		}
		if kept != 1 {
			t.Errorf("%s: the purge took a payload its own dead letter still describes: %d rows left", tenant.Slug, kept)
		}
	}

	// Operator review clears one of the two, which is what the ledger is for.
	// Nothing now refers to that row, so the age rule takes it again — and the
	// other tenant's dead letter, in another tenant's ledger, must not shelter it.
	if _, err := admin.ExecContext(ctx, `DELETE FROM platformkit_dead_letters WHERE event_id=$1`, ids[acme.ID]); err != nil {
		t.Fatal(err)
	}
	if err := Purge(ctx, conn); err != nil {
		t.Fatalf("second purge: %v", err)
	}
	var gone int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_outbox WHERE id=$1`, ids[acme.ID]).Scan(&gone); err != nil {
		t.Fatal(err)
	}
	if gone != 0 {
		t.Errorf("a row no dead letter describes survived a purge that should have taken it: %d rows left — "+
			"the exemption is reading a dead letter that is not its own", gone)
	}
	var kept int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_outbox WHERE id=$1`, ids[other.ID]).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != 1 {
		t.Errorf("the second purge took %s's payload while its dead letter is still under review: %d rows left", other.Slug, kept)
	}
}
