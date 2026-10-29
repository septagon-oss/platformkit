package audit_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	kitoperation "github.com/septagon-oss/platformkit/kit/operation"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/pkit"
)

// TestTheHandlerThePlanCarriesWritesTheRowTheTrailKeeps asks the trail's own suite the
// question its names and refusals leave open: the plan carries a handler under the
// trail's name — does that handler put a row in the trail? A wire that keeps every name
// and hands the plan a handler that validates and returns nil records nothing for any
// event, while each refusal of an unattributable operation still reads correctly and
// every name still agrees with itself. Only a row says otherwise, so this composes the
// application, lifts the one subscription handler the resolved plan carries for the
// trail, calls it on a real tenant transaction with an operation the kernel can
// attribute, and reads that row back through the migration owner.
func TestTheHandlerThePlanCarriesWritesTheRowTheTrailKeeps(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	var wired []module.Module
	reader := pkit.NewModule("rowreader", func(w *pkit.Wiring) (module.Module, error) {
		wired = pkit.Composition(w)
		return module.Module{Name: "rowreader"}, nil
	}, pkit.After(pkit.Everything))
	if _, err := pkit.NewApp("wiredrow").Use(probeInputs, audit.Module, reader).
		Plan(pkit.Deployment{Environment: pkit.Development}); err != nil {
		t.Fatalf("the plan refused: %v", err)
	}
	var handler events.Handler
	for _, m := range wired {
		if m.Name == audit.Module.Name() && len(m.Subscriptions) == 1 {
			handler = m.Subscriptions[0].Handler
		}
	}
	if handler == nil {
		t.Fatal("no composed manifest of the trail carries one subscription handler")
	}

	tenant, person, id := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}, uuid.New(), uuid.New()
	op := kitoperation.Identity{
		ID: "op-wired", Ref: uuid.NewString(), Tenant: uuid.NewString(),
		Outcome: kitoperation.Succeeded, Revision: 3,
	}
	ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), tenant), person)
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return handler(ctx, tx, events.Event{
			Name: "audit.test_recorded", ID: id, At: time.Now().UTC(),
			Operation: op, Payload: json.RawMessage(`{"userId":"` + person.String() + `"}`),
		})
	})
	if err != nil {
		t.Fatalf("the wired handler answered an operation the kernel attributes: %v", err)
	}

	var storedID string
	var revision int64
	if err := admin.QueryRowContext(t.Context(),
		`SELECT operation_id, operation_revision FROM audit_events WHERE tenant_id = $1 AND event_id = $2`,
		tenant.ID, id).Scan(&storedID, &revision); err != nil {
		t.Fatalf("the trail holds no row for %s: the handler the plan carries records nothing: %v", id, err)
	}
	if storedID != op.ID || revision != op.Revision {
		t.Errorf("the trail row carries operation %q revision %d, not the %q revision %d the event named",
			storedID, revision, op.ID, op.Revision)
	}
}
