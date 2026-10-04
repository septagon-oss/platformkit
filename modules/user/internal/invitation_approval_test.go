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
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// TestARefusedApprovalOfAnInvitationReturnsNoRowInEitherTenant: an invited
// person is pending until they accept, so approving them is refused — in their
// own tenant as a conflict, from another tenant as nothing found — and neither
// refusal hands back a row, writes the status or publishes an event.
func TestARefusedApprovalOfAnInvitationReturnsNoRowInEitherTenant(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	svc := newService()

	var invited uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := svc.Invite(ctx, tx, "invited@example.com", "Invited Person")
		if err != nil {
			return err
		}
		invited = u.ID
		return nil
	})
	if err != nil {
		t.Fatalf("invite in acme: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		got, err := svc.ApproveRegistration(ctx, tx, invited, uuid.New())
		if !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("globex approving acme's invitation = %v, want not found", err)
		}
		if got != nil {
			t.Errorf("globex's refused approval returned a row: %+v", got)
		}
		if names := outbox(t, tx); len(names) != 0 {
			t.Errorf("globex's refused approval published %v", names)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("approve as globex: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		got, err := svc.ApproveRegistration(ctx, tx, invited, uuid.New())
		if !errors.Is(err, crud.ErrConflict) {
			t.Errorf("approving an invitation = %v, want a conflict", err)
		}
		if got != nil {
			t.Errorf("a refused approval returned a row: %+v", got)
		}
		current, err := svc.Get(ctx, tx, invited)
		if err != nil {
			return err
		}
		if current.Status != contracts.StatusInvited {
			t.Errorf("a refused approval left the account %q, want %q", current.Status, contracts.StatusInvited)
		}
		names := outbox(t, tx)
		if len(names) != 1 || names[0] != contracts.EventInvited {
			t.Errorf("acme's outbox after a refused approval = %v, want only %s", names, contracts.EventInvited)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("approve as acme: %v", err)
	}
}
