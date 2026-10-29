package contracts

import (
	"context"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Each capability below names one narrow use of Auth's session, password and role
// rows. The caller owns the transaction on every method, and an error preserves
// the underlying command contract.

// PasswordLogin checks attempt limits and verifies a password before opening a session.
type PasswordLogin interface {
	Precheck(ctx context.Context, email, ip string) Verdict
	Login(ctx context.Context, tx db.Tx[db.Tenant], email, password string, from Client) (*Session, *Identity, error)
}

// SessionEnder ends a session; an already absent session is a success.
type SessionEnder interface {
	Logout(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error
}

// IdentityReader resolves a live session in its tenant, or returns not found.
type IdentityReader interface {
	Identify(ctx context.Context, tx db.Tx[db.Tenant], session uuid.UUID, from Client) (*Identity, error)
}

// PasswordChanger verifies the current password, replaces it and ends other sessions.
type PasswordChanger interface {
	ChangePassword(ctx context.Context, tx db.Tx[db.Tenant], userID, keep uuid.UUID, current, next string) error
}

// ResetRequester limits and records enumeration-neutral password reset requests.
type ResetRequester interface {
	MayAsk(ctx context.Context, ip string) Decision
	Forget(ctx context.Context, tx db.Tx[db.Tenant], email string) error
}

// PasswordResetter limits and redeems reset proofs; enrolled factors may leave no session.
type PasswordResetter interface {
	MayRedeem(ctx context.Context, ip string) Decision
	Reset(ctx context.Context, tx db.Tx[db.Tenant], token, password string,
		from Client) (*Session, error)
}

// SessionReader lists a person's live sessions without exposing credentials.
type SessionReader interface {
	Sessions(ctx context.Context, tx db.Tx[db.Tenant], userID, current uuid.UUID) ([]*SessionListing, error)
}

// SessionRevoker revokes one session owned by the supplied person, by its public reference.
type SessionRevoker interface {
	RevokeSession(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, ref string) error
}

// AllSessionsRevoker ends every session of the supplied person, including the current one.
type AllSessionsRevoker interface {
	RevokeAllSessions(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (int, error)
}

// RoleReader lists the caller's tenant roles in name order.
type RoleReader interface {
	Roles(ctx context.Context, tx db.Tx[db.Tenant]) ([]*Role, error)
}

// RoleWriter writes declared role grants under the tenant administration lock.
type RoleWriter interface {
	SetRole(ctx context.Context, tx db.Tx[db.Tenant], name string, permissions []string, declared []tenancy.Grant) (*Role, error)
}

// RoleEditor lists and writes tenant role grants.
type RoleEditor interface {
	RoleReader
	RoleWriter
}

// RoleRepairer reads role grants and reports whether a locked role write changed them.
type RoleRepairer interface {
	Roles(ctx context.Context, tx db.Tx[db.Tenant]) ([]*Role, error)
	SetRoleChanged(ctx context.Context, tx db.Tx[db.Tenant], name string, permissions []string,
		declared []tenancy.Grant) (*Role, bool, error)
}

// SessionAdministration lists sessions and revokes one or all except the named session.
type SessionAdministration interface {
	Sessions(ctx context.Context, tx db.Tx[db.Tenant], userID, current uuid.UUID) ([]*SessionListing, error)
	RevokeSession(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, ref string) error
	RevokeSessions(ctx context.Context, tx db.Tx[db.Tenant], userID, except uuid.UUID) error
}

// SessionOpener opens a session for an externally verified account, preserving factor checks.
type SessionOpener interface {
	Open(ctx context.Context, tx db.Tx[db.Tenant], user uuid.UUID, from Client, via SignInMethod) (*Session, *Identity, error)
}
