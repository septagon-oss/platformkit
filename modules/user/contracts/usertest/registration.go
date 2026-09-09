package usertest

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func registrationCases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"pending registration retains a password without enabling sign-in": func(t *testing.T, f Fixture) {
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
		},
		"approval preserves roles and password and is silent on retry": func(t *testing.T, f Fixture) {
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
		},
		"approval cannot activate an invitation or a deactivated registration": func(t *testing.T, f Fixture) {
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
		},
		"pending review pages exclude approved accounts and retain order": func(t *testing.T, f Fixture) {
			var ids []uuid.UUID
			for _, email := range []string{"first@example.com", "second@example.com", "third@example.com"} {
				u, err := f.Service.RegisterPending(f.Ctx, f.Tx, contracts.PendingRegistration{Email: email, Password: good})
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, u.ID)
			}
			if _, err := f.Service.ApproveRegistration(f.Ctx, f.Tx, ids[1], uuid.New()); err != nil {
				t.Fatal(err)
			}
			for offset, id := range []uuid.UUID{ids[0], ids[2]} {
				p, err := f.Service.PendingRegistrations(f.Ctx, f.Tx, 1, offset)
				if err != nil || p.Total != 2 || len(p.Items) != 1 || p.Items[0].ID != id {
					t.Fatalf("pending page %d = %+v, %v", offset, p, err)
				}
			}
			for _, bounds := range [][2]int{{-1, 0}, {201, 0}, {1, -1}} {
				if _, err := f.Service.PendingRegistrations(f.Ctx, f.Tx, bounds[0], bounds[1]); !errors.Is(err, crud.ErrInvalid) {
					t.Fatalf("invalid page bounds = %v", err)
				}
			}
		},
		"pending registration validates credentials and refuses an existing address": func(t *testing.T, f Fixture) {
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
		},
	}
}
