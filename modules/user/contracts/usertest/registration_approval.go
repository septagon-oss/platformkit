package usertest

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func approvePending(t *testing.T, f Fixture) {
	u, err := f.Service.RegisterPending(f.Ctx, f.Tx, contracts.PendingRegistration{Email: "ada@example.com", Password: good, Roles: []string{"customer"}})
	if err != nil {
		t.Fatal(err)
	}
	actor := uuid.New()
	for range 2 {
		got, err := f.Service.ApproveRegistration(f.Ctx, f.Tx, u.ID, actor)
		if err != nil {
			t.Fatal(err)
		}
		if !got.CanSignIn() || got.PasswordHash != u.PasswordHash || !slices.Equal(got.Roles, u.Roles) {
			t.Fatal("approval changed registration credentials or grants")
		}
	}
	published(t, f, contracts.EventRegistrationPending, contracts.EventRegistrationApproved)
}

func refuseIneligibleApproval(t *testing.T, f Fixture) {
	invited, err := f.Service.Invite(f.Ctx, f.Tx, "invited@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Service.ApproveRegistration(f.Ctx, f.Tx, invited.ID, uuid.New()); !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("approved invitation = %v", err)
	}
	pending, err := f.Service.RegisterPending(f.Ctx, f.Tx, contracts.PendingRegistration{Email: "pending@example.com", Password: good})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Service.ApproveRegistration(f.Ctx, f.Tx, pending.ID, uuid.Nil); !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("missing approval actor = %v", err)
	}
	if _, err := f.Service.Deactivate(f.Ctx, f.Tx, pending.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Service.ApproveRegistration(f.Ctx, f.Tx, pending.ID, uuid.New()); !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("approved deactivated account = %v", err)
	}
	published(t, f, contracts.EventInvited, contracts.EventRegistrationPending, contracts.EventDeactivated)
}

func refuseInvitationApproval(t *testing.T, f Fixture) {
	invited, err := f.Service.Invite(f.Ctx, f.Tx, "invited@example.com", "Invited Person")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Service.ApproveRegistration(f.Ctx, f.Tx, invited.ID, uuid.New())
	if !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("approving an invitation = %v, want a conflict", err)
	}
	// Name the invitation so the refusal cannot suggest setting a password by hand.
	if msg := err.Error(); !strings.Contains(msg, "invitation") || strings.Contains(msg, "with a password") {
		t.Errorf("approving an invitation said %q; it names the invitation and never reads as an instruction to set a password", msg)
	}
	current, err := f.Service.Get(f.Ctx, f.Tx, invited.ID)
	if err != nil || current.Status != contracts.StatusInvited {
		t.Fatalf("a refused approval left the account %q (%v)", current.Status, err)
	}
	// Nothing was written and nothing was published: the invitation event is
	// the only one this fixture has caused.
	published(t, f, contracts.EventInvited)
}
