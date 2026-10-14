package authtest

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// TokenFixture is one key case's world: the ordinary service fixture, the
// implementation under test, and the two things a key case cannot do for
// itself — present a key as a request would, and move a key past its expiry.
type TokenFixture struct {
	Fixture
	Tokens contracts.Tokens
	// Caller is the principal a request presenting token as its bearer key would
	// carry, and false when the key is not a credential at all. The SQL harness
	// answers through Authenticate with an Authorization header.
	Caller func(token string) (tenancy.Principal, bool)
	// Expire stands the clock past one key's own expiry.
	Expire func(id uuid.UUID)
}

// TokenHarness builds one TokenFixture and calls run with it.
type TokenHarness func(t *testing.T, run func(TokenFixture))

// RunTokens is the conformance suite for a person's own keys, and above all for
// RevokeOwnToken: a key retires itself and no other, a caller without a key is
// refused, and every refusal writes and publishes nothing. Row-level security is
// not asked here — there is one tenant in a fake — so "another tenant's key" is
// held at the SQL service's own door.
func RunTokens(t *testing.T, h TokenHarness) {
	t.Helper()
	for name, run := range tokenCases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f TokenFixture) { run(t, f) })
		})
	}
}

func tokenCases() map[string]func(*testing.T, TokenFixture) {
	return map[string]func(*testing.T, TokenFixture){
		"a key retires itself": func(t *testing.T, f TokenFixture) {
			ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
			key := issueKey(t, f, ada, "Phone")
			caller := present(t, f, key.Token)
			if caller.TokenID != key.ID || caller.UserID != ada {
				t.Fatalf("the key's principal is %+v, want the key %s of %s", caller, key.ID, ada)
			}
			if err := f.Tokens.RevokeOwnToken(f.Ctx, f.Tx, caller); err != nil {
				t.Fatalf("RevokeOwnToken: %v", err)
			}
			if _, ok := f.Caller(key.Token); ok {
				t.Error("a key that retired itself is still a credential")
			}
			if n := revocations(f); n != 1 {
				t.Errorf("%d revocation events, want 1", n)
			}
			if revokedAt(t, f, ada, key.ID) == nil {
				t.Error("the list does not show the key as revoked")
			}
		},
		"a caller with no key is refused and no key is touched": func(t *testing.T, f TokenFixture) {
			ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
			key := issueKey(t, f, ada, "Phone")
			err := f.Tokens.RevokeOwnToken(f.Ctx, f.Tx, tenancy.Principal{UserID: ada, Roles: []string{contracts.RoleAdmin}})
			if !errors.Is(err, contracts.ErrCallerWithoutKey) {
				t.Fatalf("a session's self-revoke = %v, want ErrCallerWithoutKey", err)
			}
			if _, ok := f.Caller(key.Token); !ok || revokedAt(t, f, ada, key.ID) != nil {
				t.Error("a refused self-revoke ended the holder's key")
			}
			if n := revocations(f); n != 0 {
				t.Errorf("a refusal published %d revocation events", n)
			}
		},
		"one key revokes itself and no other": func(t *testing.T, f TokenFixture) {
			ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
			a, b := issueKey(t, f, ada, "Phone"), issueKey(t, f, ada, "Laptop")
			if err := f.Tokens.RevokeOwnToken(f.Ctx, f.Tx, present(t, f, a.Token)); err != nil {
				t.Fatalf("RevokeOwnToken: %v", err)
			}
			if _, ok := f.Caller(a.Token); ok {
				t.Error("the key that retired itself still authenticates")
			}
			other := present(t, f, b.Token)
			if other.TokenID != b.ID || !slices.Equal(other.Permissions, []string{"widget:read"}) {
				t.Errorf("the holder's other key answers %+v, want itself with widget:read", other)
			}
			if revokedAt(t, f, ada, b.ID) != nil || revocations(f) != 1 {
				t.Error("retiring one key touched the holder's other key")
			}
		},
		"a principal naming another person's key is refused as not found": func(t *testing.T, f TokenFixture) {
			ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
			bhavna := f.User("bhavna@acme.example.com", Password, contracts.RoleAdmin)
			mine, theirs := issueKey(t, f, ada, "Phone"), issueKey(t, f, bhavna, "Phone")
			err := f.Tokens.RevokeOwnToken(f.Ctx, f.Tx, tenancy.Principal{UserID: ada, TokenID: theirs.ID})
			if !errors.Is(err, crud.ErrNotFound) {
				t.Fatalf("a principal naming another person's key = %v, want crud.ErrNotFound", err)
			}
			for _, key := range []*contracts.IssuedToken{mine, theirs} {
				if _, ok := f.Caller(key.Token); !ok {
					t.Errorf("the refusal ended %s", key.Name)
				}
			}
			if n := revocations(f); n != 0 {
				t.Errorf("a refusal published %d revocation events", n)
			}
		},
		"an id that names nothing answers the same answer": func(t *testing.T, f TokenFixture) {
			ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
			err := f.Tokens.RevokeOwnToken(f.Ctx, f.Tx, tenancy.Principal{UserID: ada, TokenID: uuid.New()})
			if !errors.Is(err, crud.ErrNotFound) {
				t.Fatalf("an id nobody issued = %v, want crud.ErrNotFound", err)
			}
		},
		"the same key answers gone the second time": func(t *testing.T, f TokenFixture) {
			ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
			caller := present(t, f, issueKey(t, f, ada, "Phone").Token)
			if err := f.Tokens.RevokeOwnToken(f.Ctx, f.Tx, caller); err != nil {
				t.Fatalf("RevokeOwnToken: %v", err)
			}
			if err := f.Tokens.RevokeOwnToken(f.Ctx, f.Tx, caller); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("a second self-revoke = %v, want crud.ErrNotFound", err)
			}
			if n := revocations(f); n != 1 {
				t.Errorf("%d revocation events for one key, want 1", n)
			}
		},
		"a management revoke of an already-stopped key is still success": func(t *testing.T, f TokenFixture) {
			ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
			key := issueKey(t, f, ada, "Phone")
			for range 2 {
				if err := f.Tokens.RevokeToken(f.Ctx, f.Tx, ada, key.ID); err != nil {
					t.Fatalf("RevokeToken: %v", err)
				}
			}
			if n := revocations(f); n != 1 {
				t.Errorf("%d revocation events for one key, want 1", n)
			}
		},
		"a key past its expiry answers gone and the trail says nothing": func(t *testing.T, f TokenFixture) {
			ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
			key := issueKey(t, f, ada, "Phone")
			caller := present(t, f, key.Token)
			f.Expire(key.ID)
			if err := f.Tokens.RevokeOwnToken(f.Ctx, f.Tx, caller); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("an expired key's self-revoke = %v, want crud.ErrNotFound", err)
			}
			if n := revocations(f); n != 0 {
				t.Errorf("an expired key published %d revocation events", n)
			}
		},
		"a key that has lost every grant can still retire itself": func(t *testing.T, f TokenFixture) {
			f.Role("deployer", "widget:read")
			ada := f.User("ada@acme.example.com", Password, "deployer")
			key := issueKey(t, f, ada, "Phone")
			if _, err := f.Service.SetRole(f.Ctx, f.Tx, "deployer", []string{"widget:manage"}, Declared); err != nil {
				t.Fatalf("narrow the role: %v", err)
			}
			caller := present(t, f, key.Token)
			if len(caller.Permissions) != 0 {
				t.Fatalf("the key still carries %v after its holder lost the grant", caller.Permissions)
			}
			if err := f.Tokens.RevokeOwnToken(f.Ctx, f.Tx, caller); err != nil {
				t.Fatalf("a key with no live scope could not retire itself: %v", err)
			}
			if n := revocations(f); n != 1 {
				t.Errorf("%d revocation events, want 1", n)
			}
		},
	}
}

func issueKey(t *testing.T, f TokenFixture, user uuid.UUID, name string) *contracts.IssuedToken {
	t.Helper()
	issued, err := f.Tokens.IssueToken(f.Ctx, f.Tx, user, contracts.TokenIntent{
		Name: name, Scopes: []string{"widget:read"},
	}, Declared)
	if err != nil {
		t.Fatalf("issue %s: %v", name, err)
	}
	return issued
}

func present(t *testing.T, f TokenFixture, token string) tenancy.Principal {
	t.Helper()
	caller, ok := f.Caller(token)
	if !ok {
		t.Fatal("a live key is not recognised as a caller")
	}
	return caller
}

func revocations(f TokenFixture) int {
	n := 0
	for _, name := range f.Published() {
		if name == contracts.EventAPITokenRevoked {
			n++
		}
	}
	return n
}

func revokedAt(t *testing.T, f TokenFixture, user, id uuid.UUID) *time.Time {
	t.Helper()
	keys, err := f.Tokens.ListTokens(f.Ctx, f.Tx, user)
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	for _, key := range keys {
		if key.ID == id {
			return key.RevokedAt
		}
	}
	t.Fatalf("key %s is not in its holder's list", id)
	return nil
}
