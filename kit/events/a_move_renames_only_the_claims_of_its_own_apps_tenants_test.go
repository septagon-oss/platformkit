package events_test

// A tenant belongs to one app (tenants.app), and so does every claim made in it.
// When two apps share one database, the ledger move of one app renames the claims
// of its own tenants and leaves the other app's tenants' claims where that app's
// own move will find them — otherwise the other app's consumer, asked to deliver an
// event it already handled, finds no claim under its durable and runs the handler
// again.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestEachAppHandlesItsOwnTenantsEventOnceAfterBothMove: the event of an academy
// tenant was handled before the deployment named its apps; collect moves first,
// then academy; a re-publish of that event through academy's own Consume runs no
// handler, because the claim is under academy's durable.
func TestEachAppHandlesItsOwnTenantsEventOnceAfterBothMove(t *testing.T) {
	_, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	school := tenancy.Tenant{ID: uuid.New(), Slug: "academy-school"}
	placeTenant(t, conn, school.ID, school.Slug, "")
	shop := tenantID(t, conn, "collect-shop", "collect")
	unscoped := appname.Durable(appname.Name(""), ledgerModule, ledgerEvent)
	claimRow(t, conn, unscoped, uuid.New(), shop)

	first := new(counter)
	old := memory.New()
	if err := events.Consume(ctx, conn, old, []events.Subscription{{
		Module: ledgerModule, Name: ledgerEvent,
		Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error { return first.run() },
	}}); err != nil {
		t.Fatalf("Consume the unscoped subscription: %v", err)
	}
	publish(t, conn, school, ledgerEvent, map[string]string{"why": "the course opened"})
	if err := events.Relay(ctx, conn, old); err != nil {
		t.Fatalf("relay: %v", err)
	}
	if first.count() != 1 {
		t.Fatalf("the unscoped subscription handled the event %d times, want once", first.count())
	}
	id := eventID(t, conn, school.ID, ledgerEvent)
	if err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec(`UPDATE tenants SET app = 'academy' WHERE id = ?`, school.ID).Error
	}); err != nil {
		t.Fatalf("name the app that holds the tenant: %v", err)
	}

	if _, err := events.MoveLedger(ctx, conn, "collect", "boot"); err != nil {
		t.Fatalf("MoveLedger as collect: %v", err)
	}
	if got := durables(t, conn, "platformkit_handled", id); len(got) != 1 || got[0] != unscoped {
		t.Errorf("after collect's move academy's tenant's claim sits at %v, want it left at %q for academy's move", got, unscoped)
	}
	if n := claimsUnder(t, conn, appname.Durable("collect", ledgerModule, ledgerEvent)); n != 1 {
		t.Errorf("%d claims sit under collect's durable, want its own tenant's one", n)
	}
	if _, err := events.MoveLedger(ctx, conn, "academy", "boot"); err != nil {
		t.Fatalf("MoveLedger as academy: %v", err)
	}
	scoped := appname.Durable("academy", ledgerModule, ledgerEvent)
	if got := durables(t, conn, "platformkit_handled", id); len(got) != 1 || got[0] != scoped {
		t.Errorf("after both moves academy's tenant's claim sits at %v, want %q", got, scoped)
	}

	second := new(counter)
	fresh := memory.New()
	if err := events.Consume(ctx, conn, fresh, []events.Subscription{{
		App: "academy", Module: ledgerModule, Name: ledgerEvent,
		Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error { return second.run() },
	}}); err != nil {
		t.Fatalf("Consume academy's subscription: %v", err)
	}
	unstamp(t, conn, id)
	if err := events.RelayApp(ctx, conn, fresh, "academy"); err != nil {
		t.Fatalf("relay as app academy: %v", err)
	}
	if second.count() != 0 {
		t.Fatalf("app academy's handler ran %d times for its own tenant's event that was already handled", second.count())
	}
}

// TestAMoveLeavesAnAppLessTenantsClaims: a deployment that names no app is a real
// deployment, and its tenants keep the empty app. A named app's move — and every tick
// of its ledger-move job after it — leaves those tenants' claims where the app-less
// consumer looks for them.
func TestAMoveLeavesAnAppLessTenantsClaims(t *testing.T) {
	_, conn := dbtest.Schema(t)
	appless := tenantID(t, conn, "plain-shop", "")
	unscoped := appname.Durable(appname.Name(""), ledgerModule, ledgerEvent)
	id := uuid.New()
	claimRow(t, conn, unscoped, id, appless)

	report, err := events.MoveLedger(t.Context(), conn, "collect", "job:ledger-move")
	if err != nil {
		t.Fatalf("MoveLedger as collect: %v", err)
	}
	if got := durables(t, conn, "platformkit_handled", id); len(got) != 1 || got[0] != unscoped {
		t.Errorf("collect's move put the app-less tenant's claim at %v, want it left at %q", got, unscoped)
	}
	if report != (events.MoveReport{}) {
		t.Errorf("collect's move reported %+v over a ledger that holds none of its tenants' rows", report)
	}
	if recs := movedRecords(t, conn, appless); len(recs) != 0 {
		t.Errorf("collect recorded %d moves in a tenant that is not its own", len(recs))
	}
}
