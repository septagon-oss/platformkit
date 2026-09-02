// Package authtest is the conformance suite for contracts.Service, a fake that
// passes it, and an OpenID Connect issuer a test can sign in against.
package authtest

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// Fixture is one case's world. The tenant already has the two roles SeedRoles
// installs, because that is what the tenant module's create hook does and every
// tenant that exists has been through it.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
	// User creates a user and returns their id. An empty password makes
	// somebody who has been invited and cannot sign in yet.
	User func(email, password string, roles ...string) uuid.UUID
	// Role grants permissions to a role name in this tenant.
	Role func(name string, permissions ...string)
	// Published is the names of the events published so far, in order.
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

// The two passwords every case uses.
const (
	Password = "correct horse battery staple"
	Wrong    = "incorrect horse battery staple"
)

// nobody is what a session opened for nobody is called.
var nobody = contracts.Client{UserAgent: "conformance", IP: "203.0.113.1"}

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"a password opens a session": func(t *testing.T, f Fixture) {
			id := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
			session, identity, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password, nobody)
			if err != nil {
				t.Fatalf("Login: %v", err)
			}
			if session.UserID != id || session.ID == uuid.Nil {
				t.Errorf("the session is %+v, want one for %s", session, id)
			}
			if !session.ExpiresAt.After(session.CreatedAt) {
				t.Errorf("the session expires at %s, having been created at %s", session.ExpiresAt, session.CreatedAt)
			}
			if identity.Email != "ada@acme.example.com" || !slices.Contains(identity.Roles, contracts.RoleAdmin) {
				t.Errorf("the identity is %+v", identity)
			}
			// admin holds the wildcard, seeded with the tenant.
			if !contracts.Grants(identity.Permissions, "anything:atall") {
				t.Errorf("admin's permissions are %v, want the wildcard", identity.Permissions)
			}
			published(t, f, contracts.EventLoggedIn)
		},

		"an address that differs only in case is the same person": func(t *testing.T, f Fixture) {
			f.User("ada@acme.example.com", Password)
			if _, _, err := f.Service.Login(f.Ctx, f.Tx, "  ADA@Acme.Example.COM ", Password, nobody); err != nil {
				t.Errorf("Login with the address in another case: %v", err)
			}
		},

		"a wrong password and an address nobody has answer alike": func(t *testing.T, f Fixture) {
			f.User("ada@acme.example.com", Password)
			_, _, wrong := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Wrong, nobody)
			_, _, unknown := f.Service.Login(f.Ctx, f.Tx, "nobody@acme.example.com", Password, nobody)
			if !errors.Is(wrong, contracts.ErrCredentials) || !errors.Is(unknown, contracts.ErrCredentials) {
				t.Fatalf("wrong password = %v, unknown address = %v; both are ErrCredentials", wrong, unknown)
			}
			if wrong.Error() != unknown.Error() {
				t.Errorf("the two refusals read differently: %q and %q", wrong, unknown)
			}
			published(t, f, contracts.EventLoginFailed, contracts.EventLoginFailed)
		},

		"somebody who has no password cannot sign in": func(t *testing.T, f Fixture) {
			f.User("invited@acme.example.com", "")
			if _, _, err := f.Service.Login(f.Ctx, f.Tx, "invited@acme.example.com", Password, nobody); !errors.Is(err, contracts.ErrCredentials) {
				t.Errorf("Login as an invited user = %v, want ErrCredentials", err)
			}
		},

		"too many failures locks the address, correct password included": func(t *testing.T, f Fixture) {
			f.User("ada@acme.example.com", Password)
			for i := range contracts.MaxAttempts {
				_, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Wrong, nobody)
				if !errors.Is(err, contracts.ErrCredentials) {
					t.Fatalf("failure %d = %v, want ErrCredentials", i+1, err)
				}
			}
			_, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password, nobody)
			if !errors.Is(err, contracts.ErrTooManyAttempts) {
				t.Errorf("the correct password after %d failures = %v, want ErrTooManyAttempts", contracts.MaxAttempts, err)
			}
			// Another address is untouched: the limit is per account, so one
			// person under attack does not lock out everybody else.
			f.User("grace@acme.example.com", Password)
			if _, _, err := f.Service.Login(f.Ctx, f.Tx, "grace@acme.example.com", Password, nobody); err != nil {
				t.Errorf("another address after a lockout: %v", err)
			}
		},

		"a session recognises its owner, and an unknown one does not": func(t *testing.T, f Fixture) {
			id := f.User("ada@acme.example.com", Password, contracts.RoleMember)
			session, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password, nobody)
			if err != nil {
				t.Fatalf("Login: %v", err)
			}
			identity, err := f.Service.Identify(f.Ctx, f.Tx, session.ID, nobody)
			if err != nil {
				t.Fatalf("Identify: %v", err)
			}
			if identity.UserID != id {
				t.Errorf("Identify returned %s, want %s", identity.UserID, id)
			}
			// member grants nothing, which is the point of it having a name.
			if len(identity.Permissions) != 0 {
				t.Errorf("member grants %v, want nothing", identity.Permissions)
			}
			if _, err := f.Service.Identify(f.Ctx, f.Tx, uuid.New(), nobody); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("Identify of an unknown session = %v, want ErrNotFound", err)
			}
		},

		"logging out ends the session, and again says nothing": func(t *testing.T, f Fixture) {
			f.User("ada@acme.example.com", Password)
			session, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password, nobody)
			if err != nil {
				t.Fatalf("Login: %v", err)
			}
			for range 2 {
				if err := f.Service.Logout(f.Ctx, f.Tx, session.ID); err != nil {
					t.Fatalf("Logout: %v", err)
				}
			}
			if _, err := f.Service.Identify(f.Ctx, f.Tx, session.ID, nobody); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("Identify after Logout = %v, want ErrNotFound", err)
			}
			published(t, f, contracts.EventLoggedIn, contracts.EventLoggedOut)
		},

		"a role grants exactly what it names": func(t *testing.T, f Fixture) {
			f.Role("editor", "task:read", "task:update")
			f.User("ada@acme.example.com", Password, "editor")
			held, err := f.Service.Permissions(f.Ctx, f.Tx, []string{"editor"})
			if err != nil {
				t.Fatalf("Permissions: %v", err)
			}
			if !contracts.Grants(held, "task:read") || contracts.Grants(held, "tenant:manage") {
				t.Errorf("editor holds %v; it grants what it names and nothing else", held)
			}
			// And a role nobody defined grants nothing rather than failing: a
			// user carrying a deleted role has less authority, not a broken
			// request.
			held, err = f.Service.Permissions(f.Ctx, f.Tx, []string{"ghost"})
			if err != nil || len(held) != 0 {
				t.Errorf("an undefined role = %v, %v; want nothing and no error", held, err)
			}
		},

		"a session opened for somebody already recognised is a session": func(t *testing.T, f Fixture) {
			id := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
			session, identity, err := f.Service.Open(f.Ctx, f.Tx, id, nobody)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if session.UserID != id || identity.UserID != id {
				t.Errorf("Open produced %+v / %+v", session, identity)
			}
			// Open is for a caller an identity provider vouched for: no
			// password is checked, and an account that is not active is still
			// refused.
			invited := f.User("invited@acme.example.com", "")
			if _, _, err := f.Service.Open(f.Ctx, f.Tx, invited, nobody); !errors.Is(err, contracts.ErrCredentials) {
				t.Errorf("Open for somebody who has not accepted their invitation = %v, want ErrCredentials", err)
			}
			published(t, f, contracts.EventLoggedIn)
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
