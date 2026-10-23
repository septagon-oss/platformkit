package authtest

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func refuseCallerWithoutKey(t *testing.T, f TokenFixture) {
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
}

func refuseAnotherOwnersKey(t *testing.T, f TokenFixture) {
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
}

func refuseUnknownKey(t *testing.T, f TokenFixture) {
	ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
	err := f.Tokens.RevokeOwnToken(f.Ctx, f.Tx, tenancy.Principal{UserID: ada, TokenID: uuid.New()})
	if !errors.Is(err, crud.ErrNotFound) {
		t.Fatalf("an id nobody issued = %v, want crud.ErrNotFound", err)
	}
}

func refuseExpiredKey(t *testing.T, f TokenFixture) {
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
}
