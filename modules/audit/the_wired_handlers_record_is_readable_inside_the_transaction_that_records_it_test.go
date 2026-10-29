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

// TestTheWiredHandlersRecordIsReadableInsideTheTransactionThatRecordsIt holds both
// halves of one question in one case, so neither half can be answered by the other:
// the trail's recorder writes on the transaction it is handed. Standing inside that
// transaction before it ends is the first half; leaving nothing behind when that
// transaction refuses is the second. Each half guards the other against passing for
// the wrong reason — a recorder that records nothing keeps a no-row-after-refusal
// answer green, and a recorder that writes on a connection of its own keeps an
// in-transaction read green — so the two together say the row is the caller's.
func TestTheWiredHandlersRecordIsReadableInsideTheTransactionThatRecordsIt(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	var wired []module.Module
	reader := pkit.NewModule("ownstrail", func(w *pkit.Wiring) (module.Module, error) {
		wired = pkit.Composition(w)
		return module.Module{Name: "ownstrail"}, nil
	}, pkit.After(pkit.Everything))
	if _, err := pkit.NewApp("ownstrail").Use(probeInputs, audit.Module, reader).
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
		ID: "op-in-transaction", Ref: uuid.NewString(), Tenant: uuid.NewString(),
		Outcome: kitoperation.Succeeded, Revision: 5,
	}
	refused := errors.New("this command refuses itself after the trail recorded")
	ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), tenant), person)
	var inside int64
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := handler(ctx, tx, events.Event{
			Name: "audit.test_recorded", ID: id, At: time.Now().UTC(),
			Operation: op, Payload: json.RawMessage(`{"userId":"` + person.String() + `"}`),
		}); err != nil {
			return err
		}
		// Read the trail the way the trail's own List does — through the transaction
		// that recorded, before anything ends. The row stands here or it is not this
		// transaction's row.
		if err := tx.DB().Table("audit_events").
			Where("tenant_id = ? AND event_id = ?", tenant.ID, id).Count(&inside).Error; err != nil {
			return err
		}
		return refused
	})
	if !errors.Is(err, refused) {
		t.Fatalf("the run answered %v, not the refusal the command gave after the record: %v", err, refused)
	}
	if inside != 1 {
		t.Errorf("the transaction that recorded reads %d row(s) for %s from its own trail: the recorder writes somewhere other than the transaction it is handed", inside, id)
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
