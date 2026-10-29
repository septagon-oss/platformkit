package internal_test

// What Record owes a caller that keeps going after a refusal has two halves: the refused record
// leaves no row, and the record after it leaves one. The case that holds the first half writes
// nothing after the refusal, and the case that holds the second counts only the operation the
// caller wrote afterwards, so a Record that stored the refused event too and answered correctly
// for the caller's own row is green under both. This asks one transaction for both halves and
// counts the tenant's whole trail, the one reading that can see a row for the refused call while
// still requiring the caller's row to have arrived.

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

func TestTheTrailOfACallerThatCarriedOnHoldsOnlyTheRowItWrote(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()

	// Only the revision is missing on the first cause, so it is the kernel that has to refuse it
	// and not one of the trail's columns. The second cause is one the trail can attribute.
	refusedOp := kitoperation.Identity{ID: "op-refused-in-the-middle", Ref: uuid.NewString(),
		Tenant: acme.ID.String(), Outcome: kitoperation.Succeeded}
	carriedOp := kitoperation.Identity{ID: "op-the-caller-carried-on-with", Ref: uuid.NewString(),
		Tenant: acme.ID.String(), Outcome: kitoperation.Succeeded, Revision: 3}

	var refusal, following error
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		refusal = svc.Record(ctx, tx, trailEvent("task.task.completed", refusedOp, db.Now()))
		following = svc.Record(ctx, tx, trailEvent("task.task.completed", carriedOp, db.Now()))
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
		t.Fatalf("the record that followed the refusal in the same transaction failed: %v", following)
	}
	rows, total := askTrail(t, conn, acme, svc, contracts.Query{})
	var written []string
	for _, row := range rows {
		if row.OperationID != nil {
			written = append(written, *row.OperationID)
		}
	}
	if total != 1 || len(written) != 1 || written[0] != carriedOp.ID {
		t.Errorf("the trail of a caller that carried on holds %d row(s) %v, want the one row it wrote after the refusal under operation %s",
			total, written, carriedOp.ID)
	}
}
