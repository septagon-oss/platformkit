package internal_test

// Record promises two things, in one order: it refuses a cause the kernel cannot attribute, and
// it stops a redelivery from writing a second row. The refusal is this function's first statement
// and the (tenant_id,event_id) key its last line, and the double this module publishes mirrors
// that order — a case beside the double holds the double to it. Nothing held it here: the replay
// case at this level carries no operation at all, and every operation refusal records each event
// once. A Record that answered "already have it" before it looked at the cause would tell a
// consumer its unattributable cause was accepted while the double went on refusing the same call
// — one contract, two answers, and the consumer cannot tell which half it reached.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	kitoperation "github.com/septagon-oss/platformkit/kit/operation"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

// TestTheTrailRefusesAnUnattributableOperationOfAnEventItAlreadyStores records one event by an
// operation the kernel can attribute, then replays that same event id with a ref that is not a
// UUID. The replay must come back refused with the kernel's own reason, it must write no row, and
// the row the accepted call wrote must still be readable by that operation — so the trail refuses
// a cause it cannot attribute whether or not it has already seen the event.
func TestTheTrailRefusesAnUnattributableOperationOfAnEventItAlreadyStores(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()

	granted := kitoperation.Identity{ID: "op-already-stored", Ref: uuid.NewString(),
		Tenant: acme.ID.String(), Outcome: kitoperation.Succeeded, Revision: 4}
	stored := trailEvent("task.task.completed", granted, db.Now())
	if err := recordOnce(t, conn, svc, stored); err != nil {
		t.Fatalf("the trail refused an operation it can attribute: %v", err)
	}

	unattributable := granted
	unattributable.Ref = "not-a-uuid"
	replayed := stored
	replayed.Operation = unattributable
	err := recordOnce(t, conn, svc, replayed)
	switch {
	case err == nil:
		t.Error("the trail accepted a replayed event whose cause it cannot attribute, an operation the kernel refuses")
	case !strings.Contains(err.Error(), "operationref"):
		t.Errorf("the trail refused the replay with %q, which names no fault of the operation", err)
	}

	rows, total := askTrail(t, conn, acme, svc, contracts.Query{OperationRef: uuid.MustParse(granted.Ref)})
	if total != 1 || len(rows) != 1 || rows[0].OperationID == nil || *rows[0].OperationID != granted.ID {
		t.Errorf("after the refusal the trail answers %d row(s) of operation %s (%v), want the one row the accepted call wrote",
			total, granted.ID, err)
	}
	if _, total := askTrail(t, conn, acme, svc, contracts.Query{}); total != 1 {
		t.Errorf("the refused replay left the tenant's trail at %d rows, want only the one row the accepted call wrote", total)
	}
}

// recordOnce delivers one event in a transaction of its own, which is the shape of a redelivery:
// the first committed, and the idempotence key is all that stands between this call and a second
// row. A panic is reported as the refusal it should have been, so a guard that broke reads as a
// wrong answer rather than as a lost test run.
func recordOnce(t *testing.T, conn *db.Conn, svc *internal.Service, ev events.Event) (err error) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("the trail panicked instead of refusing: %v", p)
		}
	}()
	return db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Record(ctx, tx, ev)
	})
}
