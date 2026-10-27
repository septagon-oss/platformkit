package audit_test

import (
	"context"
	"encoding/json"
	"errors"
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

// TestTheWiredHandlersRowLeavesWithTheTransactionThatRefusesTheCommand asks the
// trail's recorder what the question "does it write a row" cannot answer: whether
// the row it writes is the command's own or a connection of its own. A recorder
// that opens its own pool writes the trail for a delivery whose command then fails,
// so the trail holds an event whose effect never committed — and it holds it while
// every name, refusal and row-check still reads correctly. The case the row-check
// makes is answered by the insert being on the transaction the handler is handed;
// this answers it by refusing the command after that handler returns nil, and
// requires the record to leave with the rollback.
func TestTheWiredHandlersRowLeavesWithTheTransactionThatRefusesTheCommand(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	var wired []module.Module
	reader := pkit.NewModule("rollbackreader", func(w *pkit.Wiring) (module.Module, error) {
		wired = pkit.Composition(w)
		return module.Module{Name: "rollbackreader"}, nil
	}, pkit.After(pkit.Everything))
	if _, err := pkit.NewApp("rolledback").Use(probeInputs, audit.Module, reader).
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
		ID: "op-rolled-back", Ref: uuid.NewString(), Tenant: uuid.NewString(),
		Outcome: kitoperation.Succeeded, Revision: 7,
	}
	refused := errors.New("this command refuses itself after the trail was asked to record")
	ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), tenant), person)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := handler(ctx, tx, events.Event{
			Name: "audit.test_recorded", ID: id, At: time.Now().UTC(),
			Operation: op, Payload: json.RawMessage(`{"userId":"` + person.String() + `"}`),
		}); err != nil {
			return err
		}
		return refused
	}); !errors.Is(err, refused) {
		t.Fatalf("the run answered %v, not the refusal the command gave after the record: %v", err, refused)
	}

	var rows int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND event_id = $2`,
		tenant.ID, id).Scan(&rows); err != nil {
		t.Fatalf("the trail could not be read for %s: %v", id, err)
	}
	if rows != 0 {
		t.Errorf("the refused command left %d trail row(s) for %s: the recorder writes somewhere the rollback does not reach", rows, id)
	}
}
