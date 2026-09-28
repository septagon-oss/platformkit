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
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

// TestAnotherTenantAssignsNothingThroughTheService walks the case the port's own
// description declines (tasktest declines porttest.Elsewhere for all three
// commands because "the world is one tenant's transaction") against the one
// world where a second tenant can be stood up: a real second transaction on a
// real Postgres. The two tests the skip reason cites —
// kit/db's TestTenantIsolationIsEnforcedByPostgres and kit/crud's
// TestAnotherTenantReachesNothing — prove the mechanism over their own tables.
// This is the same claim one door higher, where the thing being refused is
// commands of contracts.Service and the row is a task: an actor the module's own
// policy admits, acting inside a tenant that does not own the row.
//
// It asserts the refusal and the row the refusal left:
//
//   - the visitor's Assign and CheckSLA are refused, and the refusal is the
//     not-found a row of another tenant is (tenancy is not a grant question, and
//     the row is not disclosed as existing);
//   - the owner's row is the row that was there before the refused calls — open,
//     nobody assigned, not breached — and the outbox says nothing;
//   - the visitor's own store answers nothing for that id, so the refusal is the
//     same answer as a row that was never written, which is the only answer that
//     does not disclose that somebody has one.
//
// Every assertion reads the owner's row and the outbox through the admin
// connection, so none of them depends on what the refused call answered.
func TestAnotherTenantAssignsNothingThroughTheService(t *testing.T) {
	admin, conn := dbtest.Schema(t, task.Migrations)
	owner := tenancy.Tenant{ID: uuid.New(), Slug: "review13-owner", Name: "Review thirteen owner"}
	visitor := tenancy.Tenant{ID: uuid.New(), Slug: "review13-visitor", Name: "Review thirteen visitor"}

	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), owner), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		seed := &contracts.Task{Title: "the owner's chiller"}
		if err := crud.Create(ctx, tx, seed); err != nil {
			return err
		}
		id = seed.ID
		return nil
	})
	if err != nil {
		t.Fatalf("seed the owner's task: %v", err)
	}

	// The same policy every conformance world installs: it admits
	// tasktest.Holder and refuses anybody else. The visitor below acts as that
	// holder, so nothing but the tenant can refuse these two calls.
	svc := internal.NewService()
	svc.Policy = tasktest.Policy{}
	who := uuid.New()

	err = db.Run(tasktest.As(tenancy.WithTenant(t.Context(), visitor), tasktest.Holder), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if got, err := svc.Assign(ctx, tx, id, who); err == nil {
				t.Errorf("the visitor assigned the owner's task and answered %v; a row of another tenant is not this tenant's to assign", got)
			} else if !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("the visitor's Assign returned %v, want %v: the answer that does not disclose that another tenant has a row", err, crud.ErrNotFound)
			}
			if got, err := svc.CheckSLA(ctx, tx, id); err == nil {
				t.Errorf("the visitor breached the owner's task and answered %v", got)
			} else if !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("the visitor's CheckSLA returned %v, want %v", err, crud.ErrNotFound)
			}
			// The same id, read as this tenant's row: the not-found a row that
			// was never written answers, which is what the two refusals above
			// have to agree with.
			if _, err := crud.Get[*contracts.Task](tx, id); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("the visitor read the owner's task as its own (%v); row-level security answers it as a row that is not here", err)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("the visitor's transaction: %v", err)
	}

	var status string
	var assigned bool
	var breached bool
	if err := admin.QueryRowContext(t.Context(),
		`SELECT status, assignee_id IS NOT NULL, sla_breached FROM tasks WHERE id = $1`, id).
		Scan(&status, &assigned, &breached); err != nil {
		t.Fatalf("read the owner's task back: %v", err)
	}
	if status != contracts.StatusOpen || assigned || breached {
		t.Errorf("after the refused calls the owner's task is %q, assigned=%v breached=%v; want %q, assigned=false breached=false — a refusal writes nothing",
			status, assigned, breached, contracts.StatusOpen)
	}
	var events int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE tenant_id = $1`, owner.ID).
		Scan(&events); err != nil {
		t.Fatalf("count the owner's outbox: %v", err)
	}
	if events != 0 {
		t.Errorf("the refused calls published %d events for the owner; a refusal is not news", events)
	}
}
