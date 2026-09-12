package usertest

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func verificationCases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"mailbox verification preserves the chosen password and trusted roles": func(t *testing.T, f Fixture) {
			u, err := f.Service.RegisterUnverified(f.Ctx, f.Tx, contracts.PasswordRegistration{Email: " Ada@EXAMPLE.com ", DisplayName: " Ada ", Password: good, Roles: []string{" CUSTOMER ", "customer"}})
			if err != nil {
				t.Fatal(err)
			}
			if u.Status != contracts.StatusUnverified || u.Email != "ada@example.com" || u.DisplayName != "Ada" || u.CanSignIn() || !u.CheckPassword(good) || !slices.Equal(u.Roles, contracts.Roles{"customer"}) {
				t.Fatal("unverified registration lost its normalized identity, credentials, grants or sign-in gate")
			}
			// Compare stored snapshots; database timestamps have their own precision.
			u, err = f.Service.Get(f.Ctx, f.Tx, u.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, email := range []string{"", "other@example.com"} {
				if _, err := f.Service.VerifyEmail(f.Ctx, f.Tx, u.ID, email); !errors.Is(err, crud.ErrConflict) {
					t.Fatalf("verification with another email = %v", err)
				}
			}
			if _, err := f.Service.ApproveRegistration(f.Ctx, f.Tx, u.ID, uuid.New()); !errors.Is(err, crud.ErrConflict) {
				t.Fatalf("approval bypassed mailbox verification: %v", err)
			}
			if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good+" changed"); !errors.Is(err, crud.ErrConflict) {
				t.Fatalf("password setup bypassed mailbox verification: %v", err)
			}
			page, err := f.Service.PendingRegistrations(f.Ctx, f.Tx, 0, 0)
			if err != nil || page.Total != 0 || len(page.Items) != 0 {
				t.Fatal("mailbox verification appeared in the operator approval queue")
			}
			if _, err := f.Service.RegisterUnverified(f.Ctx, f.Tx, contracts.PasswordRegistration{Email: "ADA@example.com", DisplayName: "Replacement", Password: good + " changed", Roles: []string{"admin"}}); !errors.Is(err, contracts.ErrRegistrationExists) {
				t.Fatalf("duplicate unverified registration = %v", err)
			}
			current, err := f.Service.Get(f.Ctx, f.Tx, u.ID)
			if err != nil || !reflect.DeepEqual(u, current) {
				t.Fatal("a refused operation changed the unverified account")
			}
			got, err := f.Service.VerifyEmail(f.Ctx, f.Tx, u.ID, " ADA@EXAMPLE.com ")
			if err != nil {
				t.Fatal(err)
			}
			if !got.CanSignIn() || got.PasswordHash != u.PasswordHash || !slices.Equal(got.Roles, u.Roles) || got.Email != u.Email {
				t.Fatal("verification changed the password, address or grants")
			}
			if _, err := f.Service.VerifyEmail(f.Ctx, f.Tx, u.ID, u.Email); !errors.Is(err, crud.ErrConflict) {
				t.Fatalf("verification replay = %v", err)
			}
			published(t, f, contracts.EventRegistrationUnverified, contracts.EventEmailVerified)
		},
		"verification and registration cannot replace other lifecycle states": func(t *testing.T, f Fixture) {
			for _, state := range []string{contracts.StatusInvited, contracts.StatusPending, contracts.StatusActive, contracts.StatusInactive} {
				email := state + "@example.com"
				var u *contracts.User
				var err error
				if state == contracts.StatusPending {
					u, err = f.Service.RegisterPending(f.Ctx, f.Tx, contracts.PendingRegistration{Email: email, Password: good, Roles: []string{"customer"}})
				} else {
					u, err = f.Service.Invite(f.Ctx, f.Tx, email, "Original")
				}
				if err != nil {
					t.Fatal(err)
				}
				if state == contracts.StatusActive || state == contracts.StatusInactive {
					if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good); err != nil {
						t.Fatal(err)
					}
				}
				if state == contracts.StatusInactive {
					if _, err := f.Service.Deactivate(f.Ctx, f.Tx, u.ID); err != nil {
						t.Fatal(err)
					}
				}
				u, err = f.Service.Get(f.Ctx, f.Tx, u.ID)
				if err != nil {
					t.Fatal(err)
				}
				before := f.Published()
				if _, err := f.Service.VerifyEmail(f.Ctx, f.Tx, u.ID, email); !errors.Is(err, crud.ErrConflict) {
					t.Fatalf("verification of %s account = %v", state, err)
				}
				if _, err := f.Service.RegisterUnverified(f.Ctx, f.Tx, contracts.PasswordRegistration{Email: email, DisplayName: "Replacement", Password: good + " changed", Roles: []string{"admin"}}); !errors.Is(err, contracts.ErrRegistrationExists) {
					t.Fatalf("registration over %s account = %v", state, err)
				}
				after, err := f.Service.Get(f.Ctx, f.Tx, u.ID)
				if err != nil || !reflect.DeepEqual(u, after) || !slices.Equal(before, f.Published()) {
					t.Fatalf("refused operations changed the %s account or published events", state)
				}
			}
			if _, err := f.Service.VerifyEmail(f.Ctx, f.Tx, uuid.New(), "unknown@example.com"); !errors.Is(err, crud.ErrNotFound) {
				t.Fatalf("verification of unknown account = %v", err)
			}
		},
		"unverified registration validates before insertion": func(t *testing.T, f Fixture) {
			for _, in := range []contracts.PasswordRegistration{{Email: "invalid", Password: good}, {Email: "ada@example.com", Password: short}, {Email: "ada@example.com", Password: good, Roles: []string{"invalid-role"}}} {
				if _, err := f.Service.RegisterUnverified(f.Ctx, f.Tx, in); !errors.Is(err, crud.ErrInvalid) {
					t.Fatalf("invalid unverified registration = %v", err)
				}
			}
			published(t, f)
		},
	}
}
