package internal_test

// The recorder answers an operation it cannot attribute before it reads or writes through
// the transaction. The cases that hold that rule at the trail hand it a zero db.Tx, so a
// swap of the two statements shows up there as a nil dereference, and a swap that reads
// the transaction only when it may is green under them. This asks the same question of the
// real trail with the badness in a member the columns accept — a revision of zero is a
// legal integer and every other fact of the operation is well formed — and inside a
// transaction that is allowed to commit, which is what a caller that logged the refusal
// and carried on does. A refusal that wrote first and trusted its caller to roll back
// would leave a trail row for a call that never happened.

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

func TestTheTrailLeavesNothingInTheTransactionOfACauseItRefused(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()

	// Every member the trail's columns could reject is well formed; only the revision is
	// missing, which is a legal integer and an operation the kernel cannot attribute.
	unattributable := kitoperation.Identity{ID: "op-without-a-revision", Ref: uuid.NewString(),
		Tenant: acme.ID.String(), Outcome: kitoperation.Succeeded}
	refused := trailEvent("task.task.completed", unattributable, db.Now())

	// The refusal is kept and the transaction committed, so what is asserted is what the
	// refused call left behind rather than what a rollback happened to remove.
	var refusal error
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		refusal = svc.Record(ctx, tx, refused)
		return nil
	})
	if err != nil {
		t.Fatalf("the transaction of the refused record: %v", err)
	}
	switch {
	case refusal == nil:
		t.Error("the trail accepted an operation with no revision, which the kernel cannot attribute")
	case !strings.Contains(refusal.Error(), "operation metadata"):
		t.Errorf("the trail refused with %q, which names no refusal of the operation itself", refusal)
	}
	if _, total := askTrail(t, conn, acme, svc, contracts.Query{}); total != 0 {
		t.Errorf("the refused record left %d row(s) in acme's committed trail, want none it had not written", total)
	}
}
