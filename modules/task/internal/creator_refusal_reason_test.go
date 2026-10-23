package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

// TestCreatorRefusalKeepsItsReasonReachable pins that a refused create answers
// ErrInvalid and still exposes the *fault.Reason the entity wrote, so a form can
// render the refusal in the reader's language.
func TestCreatorRefusalKeepsItsReasonReachable(t *testing.T) {
	admin, conn := dbtest.Schema(t, task.Migrations)
	_ = admin
	svc := internal.NewService()
	svc.Policy = tasktest.Policy{}
	err := db.Run(tasktest.As(tenancy.WithTenant(t.Context(), acme), tasktest.Holder), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.Create(ctx, tx, contracts.CreateTask{})
			if !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("a blank create answers %v, want ErrInvalid", err)
			}
			var reason *fault.Reason
			if !errors.As(err, &reason) || reason.Key == "" {
				t.Errorf("the refusal's reason is unreachable: %v", err)
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
}
