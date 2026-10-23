package usertest

import (
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func activateWithPassword(t *testing.T, f Fixture) {
	u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "Ada")
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	back, err := f.Service.Get(f.Ctx, f.Tx, u.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if back.Status != contracts.StatusActive || !back.CanSignIn() {
		t.Errorf("after SetPassword the user is %q and CanSignIn is %v", back.Status, back.CanSignIn())
	}
	if !back.CheckPassword(good) || back.CheckPassword(good+"!") {
		t.Error("the stored hash does not verify the password it was made from, or verifies another")
	}
	if strings.Contains(back.PasswordHash, good) {
		t.Error("the password is in the stored hash")
	}
	published(t, f, contracts.EventInvited, contracts.EventPasswordSet)
}

func refuseShortPassword(t *testing.T, f Fixture) {
	u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, short); !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("SetPassword with %d characters = %v, want ErrInvalid", len(short), err)
	}
	published(t, f, contracts.EventInvited)
}

func repeatPasswordChange(t *testing.T, f Fixture) {
	u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	for range 2 {
		if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good); err != nil {
			t.Fatalf("SetPassword: %v", err)
		}
	}
	// Two events, on purpose: somebody who changed their password twice
	// has to see both in their own audit trail.
	published(t, f, contracts.EventInvited, contracts.EventPasswordSet, contracts.EventPasswordSet)
}

func refuseDeactivatedPassword(t *testing.T, f Fixture) {
	u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if _, err := f.Service.Deactivate(f.Ctx, f.Tx, u.ID); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good); !errors.Is(err, crud.ErrConflict) {
		t.Errorf("SetPassword on a deactivated user = %v, want ErrConflict", err)
	}
}
