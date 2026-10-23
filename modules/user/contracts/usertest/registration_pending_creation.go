package usertest

import (
	"errors"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func registerPending(t *testing.T, f Fixture) {
	u, err := f.Service.RegisterPending(f.Ctx, f.Tx, contracts.PendingRegistration{Email: " Ada@EXAMPLE.com ", DisplayName: " Ada ", Password: good, Roles: []string{"customer", "customer"}})
	if err != nil {
		t.Fatal(err)
	}
	if u.Status != contracts.StatusPending || u.Email != "ada@example.com" || u.DisplayName != "Ada" || u.CanSignIn() || !u.CheckPassword(good) || !slices.Equal(u.Roles, contracts.Roles{"customer"}) {
		t.Fatal("pending registration lost its normalized identity, password, roles or sign-in gate")
	}
	if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good+" changed"); !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("pending password change = %v", err)
	}
	current, err := f.Service.Get(f.Ctx, f.Tx, u.ID)
	if err != nil || current.Status != contracts.StatusPending || current.PasswordHash != u.PasswordHash {
		t.Fatal("password change bypassed approval")
	}
	published(t, f, contracts.EventRegistrationPending)
}

func validatePendingRegistration(t *testing.T, f Fixture) {
	for _, in := range []contracts.PendingRegistration{{Email: "invalid", Password: good}, {Email: "ada@example.com", Password: short}, {Email: "ada@example.com", Password: good, Roles: []string{"invalid-role"}}} {
		if _, err := f.Service.RegisterPending(f.Ctx, f.Tx, in); !errors.Is(err, crud.ErrInvalid) {
			t.Fatalf("invalid registration = %v", err)
		}
	}
	published(t, f)
	if _, err := f.Service.Invite(f.Ctx, f.Tx, "ada@example.com", "Original"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Service.RegisterPending(f.Ctx, f.Tx, contracts.PendingRegistration{Email: "ADA@example.com", Password: good}); !errors.Is(err, contracts.ErrRegistrationExists) || !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("duplicate pending registration = %v", err)
	}
	u, err := f.Service.ByEmail(f.Ctx, f.Tx, "ada@example.com")
	if err != nil || u.DisplayName != "Original" || u.Status != contracts.StatusInvited || u.PasswordHash != "" {
		t.Fatal("an email conflict aborted the transaction or changed the invitation")
	}
	published(t, f, contracts.EventInvited)
}
