// Package usertest is the conformance suite for contracts.Service, and a fake
// that passes it.
package usertest

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Fixture is one case's world: a Service, the transaction its commands take,
// and a way to see what was published.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
	// Published is the names of the events published so far, in order. It is
	// what holds an idempotent command to saying nothing the second time.
	Published func() []string
}

// Harness builds one Fixture and calls run with it.
type Harness func(t *testing.T, run func(Fixture))

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	for name, run := range cases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f Fixture) { run(t, f) })
		})
	}
}

// good is a password that passes the length rule; short is one that does not.
const (
	good  = "correct horse battery staple"
	short = "hunter2hunt"
)

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"invite creates somebody who cannot sign in yet": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "Ada@Acme.Example.com", "  Ada Lovelace  ")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if u.Status != contracts.StatusInvited {
				t.Errorf("status is %q, want %q", u.Status, contracts.StatusInvited)
			}
			if u.Email != "ada@acme.example.com" {
				t.Errorf("email is %q; an address that differs only in case is the same mailbox", u.Email)
			}
			if u.DisplayName != "Ada Lovelace" {
				t.Errorf("display name is %q, want it trimmed", u.DisplayName)
			}
			if u.CanSignIn() {
				t.Error("an invited user with no password can sign in")
			}
			published(t, f, contracts.EventInvited)
		},

		"an address belongs to one person in a tenant": func(t *testing.T, f Fixture) {
			if _, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", ""); err != nil {
				t.Fatalf("Invite: %v", err)
			}
			_, err := f.Service.Invite(f.Ctx, f.Tx, "ADA@acme.example.com", "")
			if !errors.Is(err, crud.ErrConflict) {
				t.Errorf("a second invitation to the same address = %v, want ErrConflict", err)
			}
		},

		"invite refuses something that is not an address": func(t *testing.T, f Fixture) {
			for _, bad := range []string{"", "ada", "ada@", "@acme.example.com", "ada acme@example.com"} {
				if _, err := f.Service.Invite(f.Ctx, f.Tx, bad, ""); !errors.Is(err, crud.ErrInvalid) {
					t.Errorf("Invite(%q) = %v, want ErrInvalid", bad, err)
				}
			}
		},

		"setting a password makes an invited user active": func(t *testing.T, f Fixture) {
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
		},

		"a short password is refused": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, short); !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("SetPassword with %d characters = %v, want ErrInvalid", len(short), err)
			}
			published(t, f, contracts.EventInvited)
		},

		"setting the same password again is still a password change": func(t *testing.T, f Fixture) {
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
		},

		"roles are granted, normalised, and granted once": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			got, err := f.Service.SetRoles(f.Ctx, f.Tx, u.ID, []string{" Member ", "admin", "admin"})
			if err != nil {
				t.Fatalf("SetRoles: %v", err)
			}
			if !slices.Equal([]string(got.Roles), []string{"admin", "member"}) {
				t.Errorf("roles are %v, want them trimmed, lower-cased, deduplicated and sorted", got.Roles)
			}
			// The same set in another order is the same set: no write, no event.
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, u.ID, []string{"member", "admin"}); err != nil {
				t.Fatalf("SetRoles again: %v", err)
			}
			published(t, f, contracts.EventInvited, contracts.EventRolesSet)
		},

		"a role that is not an identifier is refused": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, u.ID, []string{"Site Admin"}); !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("SetRoles with a role that is not an identifier = %v, want ErrInvalid", err)
			}
		},

		"deactivating stops a sign-in, and twice says nothing": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good); err != nil {
				t.Fatalf("SetPassword: %v", err)
			}
			for range 2 {
				got, err := f.Service.Deactivate(f.Ctx, f.Tx, u.ID)
				if err != nil {
					t.Fatalf("Deactivate: %v", err)
				}
				if got.Status != contracts.StatusInactive || got.CanSignIn() {
					t.Errorf("a deactivated user is %q and CanSignIn is %v", got.Status, got.CanSignIn())
				}
			}
			published(t, f, contracts.EventInvited, contracts.EventPasswordSet, contracts.EventDeactivated)
		},

		"a deactivated user cannot be given a password": func(t *testing.T, f Fixture) {
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
		},

		"an address finds its person, whatever the case": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			got, err := f.Service.ByEmail(f.Ctx, f.Tx, "  ADA@Acme.Example.COM ")
			if err != nil || got.ID != u.ID {
				t.Errorf("ByEmail = %v, %v; want the invited user", got, err)
			}
			if _, err := f.Service.ByEmail(f.Ctx, f.Tx, "nobody@acme.example.com"); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("ByEmail of an address nobody has = %v, want ErrNotFound", err)
			}
		},

		"an unknown id is not found": func(t *testing.T, f Fixture) {
			id := uuid.New()
			if _, err := f.Service.Get(f.Ctx, f.Tx, id); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("Get of an unknown user = %v, want ErrNotFound", err)
			}
			if err := f.Service.SetPassword(f.Ctx, f.Tx, id, good); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("SetPassword on an unknown user = %v, want ErrNotFound", err)
			}
			if _, err := f.Service.Deactivate(f.Ctx, f.Tx, id); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("Deactivate of an unknown user = %v, want ErrNotFound", err)
			}
		},
	}
}

func published(t *testing.T, f Fixture, want ...string) {
	t.Helper()
	if f.Published == nil {
		return
	}
	if got := f.Published(); !slices.Equal(got, want) {
		t.Errorf("published %v, want %v", got, want)
	}
}
