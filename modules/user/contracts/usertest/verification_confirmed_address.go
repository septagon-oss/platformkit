package usertest

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func confirmProviderAddress(t *testing.T, f Fixture) {
	u, err := f.Service.Invite(f.Ctx, f.Tx, " Ada@EXAMPLE.com ", "Ada")
	if err != nil || u.Status != contracts.StatusInvited || u.CanSignIn() {
		t.Fatalf("invite = %v, %v; an invitation is not yet an account", u, err)
	}
	for _, email := range []string{"", "someone-else@example.com"} {
		if _, err := f.Service.ConfirmAddress(f.Ctx, f.Tx, u.ID, email); !errors.Is(err, crud.ErrConflict) {
			t.Fatalf("confirming %q against ada@example.com = %v, want a conflict", email, err)
		}
	}
	if _, err := f.Service.ConfirmAddress(f.Ctx, f.Tx, uuid.New(), "ada@example.com"); !errors.Is(err, crud.ErrNotFound) {
		t.Fatalf("confirming nobody = %v, want not found", err)
	}
	before := f.Published()
	if _, err := f.Service.ConfirmAddress(f.Ctx, f.Tx, u.ID, " ADA@EXAMPLE.com "); err != nil {
		t.Fatalf("confirm the address the provider vouched for: %v", err)
	}
	got, err := f.Service.Get(f.Ctx, f.Tx, u.ID)
	if err != nil || got.Status != contracts.StatusActive || got.CanSignIn() || got.PasswordHash != "" || got.Email != "ada@example.com" {
		t.Fatalf("the confirmed account = %+v, %v: active, still no password, the same address", got, err)
	}
	if after := f.Published(); len(after) != len(before)+1 || after[len(after)-1] != contracts.EventEmailVerified {
		t.Fatalf("confirming published %v, want one more event and the mailbox confirmation", after)
	}
	// The second sign-in through the same door says nothing: the claim was
	// already made, and a trail that repeats it would read as two facts.
	before = f.Published()
	if again, err := f.Service.ConfirmAddress(f.Ctx, f.Tx, u.ID, "ada@example.com"); err != nil || again.Status != contracts.StatusActive {
		t.Fatalf("confirming an active account = %v, %v", again, err)
	}
	if !slices.Equal(before, f.Published()) {
		t.Fatalf("the replay published %v, want nothing", f.Published())
	}
	// A closed account and an account under review are not opened by an
	// identity provider that happens to know the address.
	for _, state := range []string{"inactive", "pending"} {
		email := state + "@example.com"
		var closed uuid.UUID
		if state == "inactive" {
			invited, err := f.Service.Invite(f.Ctx, f.Tx, email, state)
			if err != nil {
				t.Fatalf("invite the subject: %v", err)
			}
			closed = invited.ID
			if _, err := f.Service.Deactivate(f.Ctx, f.Tx, closed); err != nil {
				t.Fatalf("deactivate the subject: %v", err)
			}
		} else {
			pending, err := f.Service.RegisterPending(f.Ctx, f.Tx, contracts.PendingRegistration{
				Email: email, DisplayName: state, Password: good, Roles: []string{"customer"},
			})
			if err != nil {
				t.Fatalf("register the pending subject: %v", err)
			}
			closed = pending.ID
		}
		made := f.Published()
		if _, err := f.Service.ConfirmAddress(f.Ctx, f.Tx, closed, email); !errors.Is(err, crud.ErrConflict) {
			t.Fatalf("confirming a %s account = %v, want a conflict", state, err)
		}
		if !slices.Equal(made, f.Published()) {
			t.Fatalf("confirming a %s account published %v, want nothing", state, f.Published())
		}
	}
}
