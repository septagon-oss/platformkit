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

// TestTheWiredTrailKeepsEveryOperationFactTheEventCarried holds all six facts the
// recorder copies off an operation — its id, ref, own tenant, person, outcome and
// revision — in the six columns they land in. The trail answers two different
// questions from them: "what caused this event", read off the row, and "what did
// this operation cause", which is the module's operationRef filter and the column
// 000070 indexes. The two lists are written apart — a string of column names built
// up beside a positional list of values — so a fact in the wrong slot, or left out,
// keeps every name, refusal and row-count right and answers the second question with
// nothing at all.
func TestTheWiredTrailKeepsEveryOperationFactTheEventCarried(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	var wired []module.Module
	reader := pkit.NewModule("facts", func(w *pkit.Wiring) (module.Module, error) {
		wired = pkit.Composition(w)
		return module.Module{Name: "facts"}, nil
	}, pkit.After(pkit.Everything))
	if _, err := pkit.NewApp("facts").Use(probeInputs, audit.Module, reader).
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

	// Every fact carries its own value: two facts written to one column, or one written
	// to two, is then a wrong answer rather than an invisible one.
	tenant, person, id := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}, uuid.New(), uuid.New()
	op := kitoperation.Identity{
		ID: "op-six-facts", Ref: uuid.NewString(), Tenant: uuid.NewString(),
		Person: uuid.NewString(), Outcome: kitoperation.Succeeded, Revision: 11,
	}
	ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), tenant), person)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return handler(ctx, tx, events.Event{
			Name: "audit.test_recorded", ID: id, At: time.Now().UTC(),
			Operation: op, Payload: json.RawMessage(`{"userId":"` + person.String() + `"}`),
		})
	}); err != nil {
		t.Fatalf("the wired handler answered an operation the kernel attributes: %v", err)
	}

	columns := [...]string{"operation_id", "operation_ref", "operation_tenant_id",
		"operation_person_id", "operation_outcome", "operation_revision"}
	var stored [6]string
	err := admin.QueryRowContext(t.Context(),
		`SELECT COALESCE(operation_id, ''), COALESCE(operation_ref::text, ''),
		        COALESCE(operation_tenant_id::text, ''), COALESCE(operation_person_id::text, ''),
		        COALESCE(operation_outcome, ''), COALESCE(operation_revision::text, '')
		   FROM audit_events WHERE tenant_id = $1 AND event_id = $2`, tenant.ID, id).
		Scan(&stored[0], &stored[1], &stored[2], &stored[3], &stored[4], &stored[5])
	if err != nil {
		t.Fatalf("the trail holds no row for %s: the handler the plan carries records nothing: %v", id, err)
	}
	for i, want := range [6]string{op.ID, op.Ref, op.Tenant, op.Person, string(op.Outcome), "11"} {
		if stored[i] != want {
			t.Errorf("the trail stores %s = %q, not the %q the event carried: the row no longer says what caused it",
				columns[i], stored[i], want)
		}
	}

	// The other question, the one asked from the operation side: the trail is found by
	// the occurrence, which is the only use a person has for these columns.
	var found int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND operation_ref = $2`,
		tenant.ID, op.Ref).Scan(&found); err != nil {
		t.Fatalf("the trail could not be asked what operation %s caused: %v", op.Ref, err)
	}
	if found != 1 {
		t.Errorf("asked for operation %s the trail answers %d row(s), want the one it wrote: the occurrence the event named is not where the query looks", op.Ref, found)
	}
}
