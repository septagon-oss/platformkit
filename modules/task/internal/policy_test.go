package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

type refusingPolicy struct{ calls int }

func (p *refusingPolicy) Decide(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	p.calls++
	return tenancy.PolicyDecision{}, nil
}

func TestTaskPolicyAlsoGuardsDirectServiceCalls(t *testing.T) {
	_, conn := dbtest.Schema(t, task.Migrations)
	policy := &refusingPolicy{}
	svc := internal.NewService()
	svc.Policy = policy
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		task := &contracts.Task{Title: "Unclaimed"}
		if err := crud.Create(ctx, tx, task); err != nil {
			return err
		}
		// A worker or module calling the service has the same boundary as HTTP.
		if _, err := svc.Assign(ctx, tx, task.ID, uuid.New()); !errors.Is(err, tenancy.ErrPolicyDenied) || policy.calls != 0 {
			t.Fatalf("an absent principal was promoted to system authority: %v", err)
		}
		userCtx := tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: uuid.New()})
		if _, err := svc.Assign(userCtx, tx, task.ID, uuid.New()); !errors.Is(err, tenancy.ErrPolicyDenied) || policy.calls != 1 {
			t.Fatalf("direct assignment bypassed policy: %v", err)
		}
		if _, err := svc.Resolve(userCtx, tx, task.ID, "done"); !errors.Is(err, tenancy.ErrPolicyDenied) || policy.calls != 2 {
			t.Fatalf("direct resolution bypassed policy: %v", err)
		}
		stored, err := crud.Get[*contracts.Task](tx, task.ID)
		if err != nil {
			return err
		}
		if stored.Status != contracts.StatusOpen || stored.AssigneeID != nil {
			t.Fatalf("denied service call mutated state: %+v", stored)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
