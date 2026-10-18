package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
)

// TestEveryKernelEventReachesTheTrail publishes each event the kernel itself emits —
// module.KernelEvents and events.EventReplayed, declared under module.KernelName the
// way kit/app declares them — through the outbox, relays them, and finds each one's
// outbox id in audit_events.
func TestEveryKernelEventReachesTheTrail(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), uuid.New())

	declared := []events.Declared{events.Declare[events.ReplayRecord](events.EventReplayed)}
	for _, n := range module.KernelEvents {
		declared = append(declared, events.Declared{Name: n})
	}
	mods := module.Expand([]module.Module{
		audit.New(audit.Deps{}),
		{Name: module.KernelName, Declared: declared},
	})
	if err := module.Validate(mods); err != nil {
		t.Fatal(err)
	}
	var all []events.Declared
	for _, m := range mods {
		all = append(all, m.Emits()...)
	}
	events.DeclareAll(all)
	t.Cleanup(func() { events.DeclareAll(nil) })
	transport := memory.New()
	if err := events.Consume(t.Context(), conn, transport, mods[0].Subscriptions); err != nil {
		t.Fatal(err)
	}

	for _, d := range declared {
		var payload any = map[string]any{"userId": uuid.New()}
		if d.Name == events.EventReplayed {
			payload = events.ReplayRecord{}
		}
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, d.Name, payload)
		}); err != nil {
			t.Fatalf("publish %s: %v", d.Name, err)
		}
	}
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatal(err)
	}

	rows, err := admin.QueryContext(t.Context(), `SELECT o.name, a.event_id IS NOT NULL
		FROM platformkit_outbox o LEFT JOIN audit_events a ON a.tenant_id = o.tenant_id AND a.event_id = o.id
		WHERE o.published_at IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var name string
		var recorded bool
		if err := rows.Scan(&name, &recorded); err != nil {
			t.Fatal(err)
		}
		found[name] = recorded
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, d := range declared {
		recorded, published := found[d.Name]
		switch {
		case !published:
			t.Errorf("%s was not stamped published, so the trail was compared against nothing", d.Name)
		case !recorded:
			t.Errorf("%s was published and has no trail row", d.Name)
		}
	}
}
