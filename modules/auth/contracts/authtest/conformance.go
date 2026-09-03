// Package authtest is the conformance suite for contracts.Service, a fake that
// passes it, and an OpenID Connect issuer a test can sign in against.
package authtest

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
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
	// Sent is every notice the implementation asked to be delivered, in order.
	// A case reads one to check what it does not carry.
	Sent func() []notificationcontracts.Notice
	// Mailed is every message the implementation handed a mail server, in
	// order. A case reads the link out of one to follow it, and it is the only
	// place it can be read — which is the property, not an inconvenience.
	Mailed func() []notificationcontracts.Message
	// Sessions reports how many sessions this user has, which is how a case
	// says "and the others ended" without knowing how they are stored.
	Sessions func(user uuid.UUID) int
}

// Declared is the permission catalogue the cases hand to SetRole: two ordinary
// permissions and one operator's. It stands in for what the kernel reads off
// every manifest, and the names are deliberately not any real module's — what
// is under test is the rule, not the catalogue.
var Declared = []tenancy.Grant{
	{Permission: "widget:read"},
	{Permission: "widget:manage"},
	{Permission: "fleet:manage", Operator: true},
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
			if !contracts.Grants(held, tenancy.Grant{Permission: "task:read"}) ||
				contracts.Grants(held, tenancy.Grant{Permission: "tenant:manage"}) {
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

		"changing a password needs the one in force, and ends the other sessions": func(t *testing.T, f Fixture) {
			id := f.User("ada@acme.example.com", Password)
			var keep, other uuid.UUID
			for _, into := range []*uuid.UUID{&keep, &other} {
				session, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password, nobody)
				if err != nil {
					t.Fatalf("Login: %v", err)
				}
				*into = session.ID
			}
			// The wrong current password is refused, and nothing moves: a
			// stolen cookie must not be a stolen account.
			if err := f.Service.ChangePassword(f.Ctx, f.Tx, id, keep, Wrong, "a different passphrase"); !errors.Is(err, contracts.ErrCredentials) {
				t.Fatalf("ChangePassword with the wrong current password = %v, want ErrCredentials", err)
			}
			if sessions(f, id) != 2 {
				t.Errorf("a refused change ended %d of two sessions", 2-sessions(f, id))
			}

			if err := f.Service.ChangePassword(f.Ctx, f.Tx, id, keep, Password, "a different passphrase"); err != nil {
				t.Fatalf("ChangePassword: %v", err)
			}
			if _, err := f.Service.Identify(f.Ctx, f.Tx, other, nobody); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("the other session survived a password change: %v", err)
			}
			if _, err := f.Service.Identify(f.Ctx, f.Tx, keep, nobody); err != nil {
				t.Errorf("the session that asked for the change was ended too: %v", err)
			}
			if _, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", "a different passphrase", nobody); err != nil {
				t.Errorf("the new password does not work: %v", err)
			}
		},

		"asking to reset a password does the same work whoever asks": func(t *testing.T, f Fixture) {
			f.User("ada@acme.example.com", Password)
			for _, address := range []string{"ada@acme.example.com", "nobody@acme.example.com"} {
				if err := f.Service.Forget(f.Ctx, f.Tx, address); err != nil {
					t.Errorf("Forget(%q) = %v, want nil however unknown the address", address, err)
				}
			}
			// One event each, and nothing else: no lookup, no token, no mail.
			// An address somebody has and an address nobody has cost the same,
			// which is what stops the public route being an enumeration oracle
			// with a stopwatch — 2.1 ms against 0.9 ms was the measurement that
			// moved the lookup into the worker.
			published(t, f, contracts.EventResetRequested, contracts.EventResetRequested)
			if got := mailed(f); len(got) != 0 {
				t.Errorf("Forget sent %d messages; the request path sends none", len(got))
			}
			if got := notices(f); len(got) != 0 {
				t.Errorf("Forget raised %d notices; the request path raises none", len(got))
			}
		},

		"the worker looks the address up, and mails only somebody who is here": func(t *testing.T, f Fixture) {
			f.User("ada@acme.example.com", Password)
			for _, address := range []string{"ada@acme.example.com", "nobody@acme.example.com"} {
				if err := f.Service.Reissue(f.Ctx, f.Tx, address); err != nil {
					t.Errorf("Reissue(%q) = %v, want nil however unknown the address", address, err)
				}
			}
			sent := mailed(f)
			if len(sent) != 1 {
				t.Fatalf("Reissue sent %d messages, want one — for the address that is here", len(sent))
			}
			if sent[0].To != "ada@acme.example.com" || TokenIn(sent[0].Body) == "" {
				t.Errorf("the message is %+v, want one to Ada carrying a token", sent[0])
			}
			// And the notice raised beside it carries no token. A notification
			// is an ordinary row — listed by a route, readable by anybody who
			// can read the table — so a link with a secret in it is a live
			// credential sitting in one. It used to be exactly that.
			for _, n := range notices(f) {
				if TokenIn(n.Link) != "" || strings.Contains(n.Body, TokenIn(sent[0].Body)) {
					t.Errorf("the notice carries the token: %+v", n)
				}
			}
			// Nothing about it reaches the outbox from here either: this module
			// publishes when a password is reset, not when somebody asks.
			published(t, f)
		},

		"one link per person per interval, however often somebody asks": func(t *testing.T, f Fixture) {
			f.User("ada@acme.example.com", Password)
			for range 5 {
				if err := f.Service.Reissue(f.Ctx, f.Tx, "ada@acme.example.com"); err != nil {
					t.Fatalf("Reissue: %v", err)
				}
			}
			// A public route that costs a mail is somebody else's inbox filled
			// by a stranger unless the recipient is capped too. The cap is on
			// the person being written to; the route's own cap is on the
			// address doing the asking.
			if got := mailed(f); len(got) != 1 {
				t.Errorf("five requests sent %d messages, want one", len(got))
			}
		},

		"the link sets a password once, and ends every session": func(t *testing.T, f Fixture) {
			id := f.User("ada@acme.example.com", Password)
			if _, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password, nobody); err != nil {
				t.Fatalf("Login: %v", err)
			}
			if err := f.Service.Reissue(f.Ctx, f.Tx, "ada@acme.example.com"); err != nil {
				t.Fatalf("Reissue: %v", err)
			}
			sent := mailed(f)
			if len(sent) != 1 {
				t.Fatalf("Reissue sent %d messages, want one", len(sent))
			}
			token := TokenIn(sent[0].Body)

			if err := f.Service.Reset(f.Ctx, f.Tx, token, "a different passphrase"); err != nil {
				t.Fatalf("Reset: %v", err)
			}
			if got := sessions(f, id); got != 0 {
				t.Errorf("%d sessions survived a reset; a reset ends every one", got)
			}
			// Once. The row is deleted rather than flagged, so the second
			// attempt is refused by the same answer an invented token gets.
			for _, second := range []string{token, "not a token at all", ""} {
				if err := f.Service.Reset(f.Ctx, f.Tx, second, "another passphrase"); !errors.Is(err, contracts.ErrCredentials) {
					t.Errorf("Reset(%q) = %v, want ErrCredentials", second, err)
				}
			}
			published(t, f, contracts.EventLoggedIn, contracts.EventPasswordReset)
			if _, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", "a different passphrase", nobody); err != nil {
				t.Errorf("the reset password does not work: %v", err)
			}
		},

		"an invitation is offered a link, and somebody who can already sign in is not": func(t *testing.T, f Fixture) {
			invited := f.User("invited@acme.example.com", "")
			active := f.User("ada@acme.example.com", Password)
			for _, id := range []uuid.UUID{invited, active, uuid.New()} {
				if err := f.Service.Offer(f.Ctx, f.Tx, id); err != nil {
					t.Errorf("Offer(%s) = %v, want nil", id, err)
				}
			}
			if got := mailed(f); len(got) != 1 {
				t.Fatalf("Offer sent %d messages, want one — for the person who cannot sign in", len(got))
			}
			// And the link works: an invitation and a reset are one mechanism.
			token := TokenIn(mailed(f)[0].Body)
			if err := f.Service.Reset(f.Ctx, f.Tx, token, "a chosen passphrase"); err != nil {
				t.Fatalf("Reset with an invitation's token: %v", err)
			}
			if _, _, err := f.Service.Login(f.Ctx, f.Tx, "invited@acme.example.com", "a chosen passphrase", nobody); err != nil {
				t.Errorf("the invited person cannot sign in: %v", err)
			}
		},

		"a role grants only permissions the application defines": func(t *testing.T, f Fixture) {
			role, err := f.Service.SetRole(f.Ctx, f.Tx, "editor", []string{"widget:read", "widget:manage"}, Declared)
			if err != nil {
				t.Fatalf("SetRole: %v", err)
			}
			if !slices.Equal([]string(role.Grants), []string{"widget:manage", "widget:read"}) {
				t.Errorf("editor grants %v, want the two it was given, sorted", role.Grants)
			}
			held, err := f.Service.Permissions(f.Ctx, f.Tx, []string{"editor"})
			if err != nil || !contracts.Grants(held, tenancy.Grant{Permission: "widget:read"}) {
				t.Errorf("editor holds %v (%v), want what SetRole wrote", held, err)
			}
			// A permission nothing defines is refused rather than written: a
			// role naming one is a grant that can never be exercised and reads,
			// to whoever wrote it, exactly like one that can.
			if _, err := f.Service.SetRole(f.Ctx, f.Tx, "editor", []string{"widget:read", "ghost:read"}, Declared); !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("SetRole with an undefined permission = %v, want ErrInvalid", err)
			}
			// And the refusal changed nothing.
			if roles, err := f.Service.Roles(f.Ctx, f.Tx); err != nil {
				t.Fatalf("Roles: %v", err)
			} else if i := slices.IndexFunc(roles, func(r *contracts.Role) bool { return r.Name == "editor" }); i < 0 ||
				!slices.Equal([]string(roles[i].Grants), []string{"widget:manage", "widget:read"}) {
				t.Errorf("the roles are %v after a refused write", roles)
			}
			published(t, f, contracts.EventRoleSet)
		},

		"an operator permission cannot be granted in a customer's tenant": func(t *testing.T, f Fixture) {
			// This tenant is a customer's — every fixture's is — so naming the
			// installation's own permission in one of its roles is refused. It
			// would be a grant that looks like authority and is not: the kernel
			// refuses an operator route here before it reads any role at all.
			_, err := f.Service.SetRole(f.Ctx, f.Tx, "operator", []string{"fleet:manage"}, Declared)
			if !errors.Is(err, crud.ErrInvalid) {
				t.Fatalf("SetRole with an operator permission = %v, want ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), "fleet:manage") {
				t.Errorf("the refusal does not name the permission: %v", err)
			}
			published(t, f)
		},
	}
}

// notices is what the implementation asked to have delivered, or nothing when
// the harness wired no recorder.
func notices(f Fixture) []notificationcontracts.Notice {
	if f.Sent == nil {
		return nil
	}
	return f.Sent()
}

// mailed is what the implementation handed a mail server, or nothing when the
// harness wired no mailbox.
func mailed(f Fixture) []notificationcontracts.Message {
	if f.Mailed == nil {
		return nil
	}
	return f.Mailed()
}

// sessions is how many the user has, or -1 when the harness cannot say.
func sessions(f Fixture, user uuid.UUID) int {
	if f.Sessions == nil {
		return -1
	}
	return f.Sessions(user)
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
