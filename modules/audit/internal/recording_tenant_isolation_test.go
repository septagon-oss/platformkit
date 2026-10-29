package internal_test

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

func TestAnOperationRefCannotReadAnotherTenantsTrail(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()
	cause := kitoperation.Identity{ID: "foreign-operation", Ref: uuid.NewString(),
		Tenant: globex.ID.String(), Outcome: kitoperation.Succeeded, Revision: 1}
	if err := recordOnce(t, conn, svc, trailEvent("task.task.completed", cause, db.Now())); err != nil {
		t.Fatalf("record event: %v", err)
	}

	rows, total := askTrail(t, conn, globex, svc, contracts.Query{OperationRef: uuid.MustParse(cause.Ref)})
	if total != 0 || len(rows) != 0 {
		t.Fatalf("tenant named by operation read the recording tenant's trail: %d row(s), want none", total)
	}
}
