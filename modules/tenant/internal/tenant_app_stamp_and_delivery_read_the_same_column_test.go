// The tenant's app is written by one package and read by another: modules/tenant
// stamps tenants.app at the create from the composition's own slug, and kit/events
// reads that column at every delivery to decide whether a handler may run in that
// tenant at all. Both statements are true in their own package's tests — the
// control plane's own case reads the stamp back, the transport cases insert their
// tenants by hand — and nothing in between says the two readings are one reading.
// They have to be: an installation whose control plane stamps a value the delivery
// does not recognise has no failing boundary, it has an app that never receives
// any of its own events, and the failure looks like a broker that is down.
//
// So this case crosses the seam. Two compositions of one module own one schema;
// each creates its tenant through the control plane — the write path a real boot
// takes, not a hand-written INSERT — and each then subscribes the same event with
// its own app. The tenant each app created is delivered to that app's handler and
// to nobody else's, in the tenant the row names.
package internal_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
)

func TestATenantTheControlPlaneStampedIsTheTenantTheDeliveryReads(t *testing.T) {
	_, conn := dbtest.Schema(t)

	// The installation's own tenant, which modules/tenant's Create now asks for before
	// it writes anything: a schema that has never been bootstrapped holds no operator
	// tenant to mirror a create's audit row into, and the create refuses. It is made by
	// a service of no app, so it sits outside every app-scoped read below — the count
	// this case asserts stays the count it was. The world gained a tenant; no
	// expectation moved. main's `installed` helper, called here for the same reason
	// every fixture in this package now calls it.
	installed(t, conn, internal.NewService(nil, nil, ""))

	const module, event = "billing", "billing.plan.created"
	apps := []appname.Name{appname.MustParse("acme"), appname.MustParse("acme-billing")}

	customers := map[appname.Name]uuid.UUID{}
	for _, app := range apps {
		slug := app.String() + "-customer"
		svc := internal.NewService(nil, nil, app)
		var id uuid.UUID
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			created, err := svc.Create(ctx, tx, contracts.NewTenant{Slug: slug, Name: slug, Host: slug + ".example.com"})
			if err != nil {
				return err
			}
			id = created.ID
			return nil
		})
		if err != nil {
			t.Fatalf("app %s creates its tenant through the control plane: %v", app, err)
		}
		customers[app] = id
	}

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	// Both apps subscribe the same module's same event, which is the shape that
	// makes the check matter: the message reaches both sinks, and only the tenant's
	// own app may run a handler for it.
	transport := memory.New()
	ran := make(chan string, 8)
	for _, app := range apps {
		sub := events.Subscription{
			App:    app,
			Module: module,
			Name:   event,
			Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
				ran <- app.String() + "/" + db.TenantOf(tx).ID.String()
				return nil
			},
		}
		if err := events.Consume(ctx, conn, transport, []events.Subscription{sub}); err != nil {
			t.Fatalf("app %s subscribes %s: %v", app, event, err)
		}
	}

	for _, app := range apps {
		ev := events.Event{ID: uuid.New(), Name: event, TenantID: customers[app], At: time.Now().UTC()}
		if err := transport.Publish(t.Context(), ev); err != nil {
			t.Fatalf("deliver %s of tenant %s: %v", event, ev.TenantID, err)
		}
	}

	// Each app's own event reaches its own handler. A third line here would be
	// one app running a handler inside the other's tenant, which is what the
	// column exists to make impossible; a missing line is an app that cannot
	// receive its own work, which is the same column read two ways.
	want := map[string]bool{
		apps[0].String() + "/" + customers[apps[0]].String(): true,
		apps[1].String() + "/" + customers[apps[1]].String(): true,
	}
	seen := map[string]int{}
	for i := 0; i < 3; i++ {
		select {
		case line := <-ran:
			seen[line]++
		case <-time.After(5 * time.Second):
			i = 3
		}
	}
	if len(seen) != len(want) {
		t.Errorf("%d deliveries ran a handler (%v), want %d: every app receives its own tenant's event and no app receives the other's",
			len(seen), keys(seen), len(want))
	}
	for line, n := range seen {
		if !want[line] {
			t.Errorf("handler ran for %s (x%d), which no app of this deployment owns: the stamp and the read are not one fact", line, n)
		}
	}
}

func keys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
