package internal_test

// The trail answers an operation it cannot attribute out of what the event carried, before it
// touches the database, so the transaction that refused call ran in stays open for the caller. A
// refusal the database produced instead would abort that transaction, and the handler that logged
// the refusal and carried on — the caller this package describes — would lose every row it wrote
// afterwards without being told. This asks the real trail for both halves in one transaction: the
// refusal, then a record the trail can attribute, with the transaction committed at the end.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	kitoperation "github.com/septagon-oss/platformkit/kit/operation"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

func TestTheTrailLeavesTheTransactionOfACallerThatCarriedOnUsable(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()

	// Both causes are well formed to the trail's columns; only the missing revision tells the
	// kernel they are not one operation, and only the kernel is asked.
	refusedOp := kitoperation.Identity{ID: "op-without-a-revision", Ref: uuid.NewString(),
		Tenant: acme.ID.String(), Outcome: kitoperation.Succeeded}
	grantedOp := kitoperation.Identity{ID: "op-after-the-refusal", Ref: uuid.NewString(),
		Tenant: acme.ID.String(), Outcome: kitoperation.Succeeded, Revision: 7}

	var refusal, following error
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		refusal = svc.Record(ctx, tx, trailEvent("task.task.completed", refusedOp, db.Now()))
		following = svc.Record(ctx, tx, trailEvent("task.task.completed", grantedOp, db.Now()))
		return nil
	})
	if err != nil {
		t.Fatalf("the transaction of a caller that carried on after the refusal: %v", err)
	}
	switch {
	case refusal == nil:
		t.Error("the trail accepted an operation with no revision, which the kernel cannot attribute")
	case !strings.Contains(refusal.Error(), "operation metadata"):
		t.Errorf("the trail refused with %q, which names no refusal of the operation itself", refusal)
	}
	if following != nil {
		t.Errorf("the record that followed the refusal in the same transaction failed: %v", following)
	}
	rows, total := askTrail(t, conn, acme, svc, contracts.Query{OperationRef: uuid.MustParse(grantedOp.Ref)})
	var written string
	if len(rows) == 1 && rows[0].OperationID != nil {
		written = *rows[0].OperationID
	}
	if total != 1 || written != grantedOp.ID {
		t.Errorf("the caller that carried on committed %d row(s) %q, want the one row it wrote after the refusal under operation %s",
			total, written, grantedOp.ID)
	}
}
