package internal_test

// A trail row belongs to the tenant whose transaction recorded it, never to the tenant its
// operation names: Record takes tenant_id from db.TenantOf(tx) and copies the operation's own
// tenant into operation_tenant_id, which List does not filter by. The row and the fact are two
// different tenants whenever an event crosses — the relay carries an operation's facts into the
// trail of the tenant that consumed it — so the read door has to answer from the row's tenant
// even when the operation it filters by names the reader. Asked by nothing, another tenant's
// trail is empty; asked by an operation ref the other tenant's own operation carried, it is the
// one question a predicate on operation_tenant_id would answer wrong.

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	kitoperation "github.com/septagon-oss/platformkit/kit/operation"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

func TestTheTrailOfAnOperationThatNamesAnotherTenantStaysInTheRecordingTenant(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()

	// One well-formed operation, and the tenant it belongs to is not the one recording it.
	cause := kitoperation.Identity{ID: "op-of-another-tenant", Ref: uuid.NewString(),
		Tenant: globex.ID.String(), Outcome: kitoperation.Succeeded, Revision: 6}
	if err := recordOnce(t, conn, svc, trailEvent("task.task.completed", cause, db.Now())); err != nil {
		t.Fatalf("the trail refused an operation it can attribute: %v", err)
	}
	ref := uuid.MustParse(cause.Ref)

	if _, total := askTrail(t, conn, acme, svc, contracts.Query{OperationRef: ref}); total != 1 {
		t.Errorf("acme, which recorded the event, asked by its operation ref answers %d row(s), want the one row it wrote", total)
	}
	if _, total := askTrail(t, conn, globex, svc, contracts.Query{OperationRef: ref}); total != 0 {
		t.Errorf("globex, the tenant the operation names and which recorded nothing, read %d row(s) of acme's trail by that operation ref, want none", total)
	}
	if _, total := askTrail(t, conn, globex, svc, contracts.Query{}); total != 0 {
		t.Errorf("globex answers its own trail with %d row(s) after acme recorded an event its operation caused, want none", total)
	}
}
