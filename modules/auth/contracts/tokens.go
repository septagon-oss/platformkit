package contracts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// APITokenDefaultLifetime and APITokenMaxLifetime. A bearer with no expiry is a
// password somebody can read out of a phone, so an issue without an expiry is
// not accepted and the default is short: thirty days for a laptop that is
// expected to be seen again, and six months as the most anyone may ask for.
// Nothing extends either one: a token that renewed itself on use would be an
// expiry in name only, which is the argument SessionMaxLifetime already makes
// about a sliding session.
const (
	APITokenDefaultLifetime = 30 * 24 * time.Hour
	APITokenMaxLifetime     = 180 * 24 * time.Hour
	// APITokenRetention is how long a key that is expired, or was revoked, stays
	// a row: the sweep takes it after that, so that "revoked" eventually means the
	// row is gone as well as the key not working now.
	APITokenRetention = 30 * 24 * time.Hour
	// APITokenTouch is how often a live token may record that it was used. The
	// same throttle sessions use: a read-only page is not a reason to take a row
	// lock on every request.
	APITokenTouch = 5 * time.Minute
)

// ErrTokenScope is IssueToken's refusal, and one answer covers three facts
// because each of them says something a caller must not learn about a tenant
// they may not be inside: the scope names a permission no module defines, the
// scope is one the caller's own roles do not grant, or the scope is an operator
// permission at a caller who is not in the operator's tenant. The message names
// the scope that failed and never why the others did not.
var ErrTokenScope = errors.New("auth: that token asks for a grant its holder does not have")

// TokenIntent is what a person asks for: a name to recognise the key by, the
// scopes it may use, and when it stops working. An empty scope list is refused
// rather than read as "everything" — a name and a list are the two things this
// command is asking for, and a default of full authority is the default that
// gets audited.
type TokenIntent struct {
	Name      string    `json:"name" maxLength:"80" example:"Deploy bot"`
	Scopes    []string  `json:"scopes" example:"task:read"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// IssuedToken is the intent, the row's metadata and the token — the token
// appearing here and nowhere else, ever. What the database holds is its hash.
type IssuedToken struct {
	APIToken
	Token string `json:"token" example:"pkit_9f2c1a…" doc:"Shown once, in this response and no other"`
}

// APIToken is one row of the list a person reads about their own keys. RevokedAt
// is set rather than the row deleted, so the trail and the list can answer "is
// the key that stopped working the one I ended, or the one that expired".
type APIToken struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt time.Time  `json:"lastUsedAt"`
	CreatedBy  uuid.UUID  `json:"createdBy"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

// Tokens is a person's own bearer tokens: made, listed, revoked. Everything here
// is about the caller, which is why no route in it takes a permission, and why
// there is no command for one person minting a key as another — an
// administrator minting an integration key for somebody else is the product's
// decision, not this module's default.
type Tokens interface {
	// IssueToken mints a key. Every scope is checked twice before anything is
	// written: against the permissions every module declares, and against the
	// ones this person's roles already grant. A token can therefore never hold
	// authority its holder does not have, and a scope that could not be
	// exercised is refused at issue rather than stored as a grant that reads like
	// one and is not — the two rules SetRole already applies to a role.
	IssueToken(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, in TokenIntent, declared []tenancy.Grant) (*IssuedToken, error)

	// ListTokens lists this person's keys, most recently issued first, and answers
	// with APIToken: no token, no hash, no prefix. A list of keys is a list of
	// credentials, and this one is a screen.
	ListTokens(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) ([]*APIToken, error)

	// RevokeToken stops one of this person's own keys. An unknown id, or one
	// belonging to another person or another tenant, is crud.ErrNotFound:
	// row-level security returns no row either way, so the refusal cannot be used
	// to find out which keys exist elsewhere. It ends the key, not the person's
	// sessions — a stolen key and a stolen laptop are different incidents, and
	// the second has its own command.
	RevokeToken(ctx context.Context, tx db.Tx[db.Tenant], userID, id uuid.UUID) error
}
