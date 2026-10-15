package authtest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// fakeKey is one issued key. The secret is kept as issued rather than hashed:
// what the hash protects is a table somebody can read, and the fake has none.
type fakeKey struct {
	contracts.APIToken
	userID uuid.UUID
	secret string
}

// IssueToken mirrors the real command's refusals, in order; bad input is crud.ErrInvalid.
func (f *Fake) IssueToken(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, in contracts.TokenIntent, declared []tenancy.Grant) (*contracts.IssuedToken, error) {
	if in.Name == "" || len(in.Name) > 80 || len(in.Scopes) == 0 {
		return nil, crud.ErrInvalid
	}
	user, err := f.Users.Get(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if user.Status != usercontracts.StatusActive {
		return nil, contracts.ErrCredentials
	}
	granted, err := contracts.CheckedPermissions(in.Scopes, declared, db.TenantOf(tx))
	if err != nil {
		return nil, err
	}
	held, err := f.Permissions(ctx, tx, user.Roles)
	if err != nil {
		return nil, err
	}
	for _, scope := range granted {
		grant := tenancy.Grant{Permission: string(scope)}
		for _, g := range declared {
			if g.Permission == string(scope) {
				grant = g
			}
		}
		if string(scope) == contracts.Wildcard || !contracts.Grants(held, grant) {
			return nil, fmt.Errorf("auth: %s is not this holder's to delegate: %w", scope, contracts.ErrTokenScope)
		}
	}
	at := db.Now()
	until := in.ExpiresAt
	if until.IsZero() {
		until = at.Add(contracts.APITokenDefaultLifetime)
	}
	if until.After(at.Add(contracts.APITokenMaxLifetime)) {
		return nil, crud.ErrInvalid
	}
	key := &fakeKey{userID: userID, secret: "pkit_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		APIToken: contracts.APIToken{ID: uuid.New(), Name: in.Name, Scopes: []string(slices.Clone(granted)),
			CreatedAt: at, ExpiresAt: until, LastUsedAt: at, CreatedBy: userID}}
	f.mu.Lock()
	f.keys[key.ID] = key
	f.mu.Unlock()
	f.record(contracts.EventAPITokenIssued)
	return &contracts.IssuedToken{APIToken: key.APIToken, Token: key.secret}, nil
}

// ListTokens answers the person's own keys, most recent first.
func (f *Fake) ListTokens(_ context.Context, _ db.Tx[db.Tenant], userID uuid.UUID) ([]*contracts.APIToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []*contracts.APIToken{}
	for _, key := range f.keys {
		if key.userID == userID {
			row := key.APIToken
			out = append(out, &row)
		}
	}
	slices.SortFunc(out, func(a, b *contracts.APIToken) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out, nil
}

// RevokeToken mirrors the real rule: a key already stopped is success and no
// second event; one that is not this person's is crud.ErrNotFound.
func (f *Fake) RevokeToken(_ context.Context, _ db.Tx[db.Tenant], userID, id uuid.UUID) error {
	f.mu.Lock()
	key, ok := f.keys[id]
	if !ok || key.userID != userID {
		f.mu.Unlock()
		return crud.ErrNotFound
	}
	stopped := key.RevokedAt != nil
	if !stopped {
		at := db.Now()
		key.RevokedAt = &at
	}
	f.mu.Unlock()
	if !stopped {
		f.record(contracts.EventAPITokenRevoked)
	}
	return nil
}

// RevokeOwnToken mirrors the real rule: the pair (TokenID, UserID) must name a
// live key, and anything else is gone.
func (f *Fake) RevokeOwnToken(_ context.Context, _ db.Tx[db.Tenant], caller tenancy.Principal) error {
	if caller.TokenID == uuid.Nil {
		return contracts.ErrCallerWithoutKey
	}
	f.mu.Lock()
	key, ok := f.keys[caller.TokenID]
	at := db.Now()
	if !ok || key.userID != caller.UserID || key.RevokedAt != nil || !key.ExpiresAt.After(at) {
		f.mu.Unlock()
		return crud.ErrNotFound
	}
	key.RevokedAt = &at
	f.mu.Unlock()
	f.record(contracts.EventAPITokenRevoked)
	return nil
}

// Bearer is the fake's bearer recognition, the principal internal.Service's
// Authenticate builds from an Authorization header: the holder, the key's id,
// and the key's scopes narrowed to what the holder's roles grant today. A
// revoked, expired or unknown key, or one whose holder is not active, is not a
// caller.
func (f *Fake) Bearer(ctx context.Context, tx db.Tx[db.Tenant], token string) (tenancy.Principal, bool, error) {
	f.mu.Lock()
	var key *fakeKey
	for _, k := range f.keys {
		if k.secret == token {
			key = k
		}
	}
	live := key != nil && key.RevokedAt == nil && key.ExpiresAt.After(db.Now())
	f.mu.Unlock()
	if !live {
		return tenancy.Principal{}, false, nil
	}
	user, err := f.Users.Get(ctx, tx, key.userID)
	if err != nil || user.Status != usercontracts.StatusActive {
		return tenancy.Principal{}, false, nil
	}
	held, err := f.Permissions(ctx, tx, user.Roles)
	if err != nil {
		return tenancy.Principal{}, false, err
	}
	scopes := make([]string, 0, len(key.Scopes))
	for _, scope := range key.Scopes {
		if contracts.Grants(held, tenancy.Grant{Permission: scope}) {
			scopes = append(scopes, scope)
		}
	}
	return tenancy.Principal{UserID: user.ID, TokenID: key.ID, Permissions: scopes}, true, nil
}

// ExpireToken stands the clock past one key's expiry.
func (f *Fake) ExpireToken(id uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if key, ok := f.keys[id]; ok {
		key.ExpiresAt = db.Now().Add(-time.Second)
	}
}
