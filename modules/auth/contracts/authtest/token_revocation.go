package authtest

import (
	"errors"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func retireOwnKey(t *testing.T, f TokenFixture) {
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
}

func retireOnlyOwnKey(t *testing.T, f TokenFixture) {
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
}

func refuseRepeatedSelfRevocation(t *testing.T, f TokenFixture) {
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
}

func repeatManagementRevocation(t *testing.T, f TokenFixture) {
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
}

func retireKeyWithoutGrants(t *testing.T, f TokenFixture) {
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
}
