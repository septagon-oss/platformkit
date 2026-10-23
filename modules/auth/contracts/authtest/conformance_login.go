package authtest

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func loginWithPassword(t *testing.T, f Fixture) {
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
	// admin holds the wildcard, seeded with the tenant: it grants every
	// ordinary permission there is.
	if !contracts.Grants(identity.Permissions, tenancy.Grant{Permission: "anything:atall"}) {
		t.Errorf("admin's permissions are %v, want the wildcard", identity.Permissions)
	}
	// And it grants no operator permission, which is the whole reason
	// that kind exists: this tenant is a customer's, admin is its
	// wildcard, and the control plane is not theirs to reach.
	if contracts.Grants(identity.Permissions, tenancy.Grant{Permission: "anything:atall", Operator: true}) {
		t.Errorf("admin's wildcard %v satisfied an operator grant", identity.Permissions)
	}
	published(t, f, contracts.EventLoggedIn)
}

func loginWithFoldedAddress(t *testing.T, f Fixture) {
	f.User("ada@acme.example.com", Password)
	if _, _, err := f.Service.Login(f.Ctx, f.Tx, "  ADA@Acme.Example.COM ", Password, nobody); err != nil {
		t.Errorf("Login with the address in another case: %v", err)
	}
}

func refuseInvalidCredentials(t *testing.T, f Fixture) {
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
}

func refusePasswordlessLogin(t *testing.T, f Fixture) {
	f.User("invited@acme.example.com", "")
	if _, _, err := f.Service.Login(f.Ctx, f.Tx, "invited@acme.example.com", Password, nobody); !errors.Is(err, contracts.ErrCredentials) {
		t.Errorf("Login as an invited user = %v, want ErrCredentials", err)
	}
}

func lockFailedLogins(t *testing.T, f Fixture) {
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
}

func identifySession(t *testing.T, f Fixture) {
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
}

func logoutSession(t *testing.T, f Fixture) {
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
}

func openRecognisedSession(t *testing.T, f Fixture) {
	id := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
	session, identity, err := f.Service.Open(f.Ctx, f.Tx, id, nobody, contracts.ViaOIDC)
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
	if _, _, err := f.Service.Open(f.Ctx, f.Tx, invited, nobody, contracts.ViaOIDC); !errors.Is(err, contracts.ErrCredentials) {
		t.Errorf("Open for somebody who has not accepted their invitation = %v, want ErrCredentials", err)
	}
	published(t, f, contracts.EventLoggedIn)
}
