package authtest

import (
	"testing"
	"time"

	"github.com/google/uuid"

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
		"a key retires itself": retireOwnKey,
		"a caller with no key is refused and no key is touched":           refuseCallerWithoutKey,
		"one key revokes itself and no other":                             retireOnlyOwnKey,
		"a principal naming another person's key is refused as not found": refuseAnotherOwnersKey,
		"an id that names nothing answers the same answer":                refuseUnknownKey,
		"the same key answers gone the second time":                       refuseRepeatedSelfRevocation,
		"a management revoke of an already-stopped key is still success":  repeatManagementRevocation,
		"a key past its expiry answers gone and the trail says nothing":   refuseExpiredKey,
		"a key that has lost every grant can still retire itself":         retireKeyWithoutGrants,
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
