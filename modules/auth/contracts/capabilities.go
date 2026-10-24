package contracts

import (
	"context"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// PasswordLogin checks attempt limits and verifies a password before opening a session.
// The caller owns the transaction; errors preserve the underlying command contract.
type PasswordLogin interface {
	Precheck(ctx context.Context, email, ip string) Verdict
	Login(ctx context.Context, tx db.Tx[db.Tenant], email, password string, from Client) (*Session, *Identity, error)
}

// SessionEnder ends a session; an already absent session is a success.
// The caller owns the transaction; errors preserve the underlying command contract.
type SessionEnder interface {
	Logout(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error
}

// IdentityReader resolves a live session in its tenant, or returns not found.
// The caller owns the transaction; errors preserve the underlying command contract.
type IdentityReader interface {
	Identify(ctx context.Context, tx db.Tx[db.Tenant], session uuid.UUID, from Client) (*Identity, error)
}

// PasswordChanger verifies the current password, replaces it and ends other sessions.
// The caller owns the transaction; errors preserve the underlying command contract.
type PasswordChanger interface {
	ChangePassword(ctx context.Context, tx db.Tx[db.Tenant], userID, keep uuid.UUID, current, next string) error
}

// ResetRequester limits and records enumeration-neutral password reset requests.
// The caller owns the transaction; errors preserve the underlying command contract.
type ResetRequester interface {
	MayAsk(ctx context.Context, ip string) Decision
	Forget(ctx context.Context, tx db.Tx[db.Tenant], email string) error
}

// PasswordResetter limits and redeems reset proofs; enrolled factors may leave no session.
// The caller owns the transaction; errors preserve the underlying command contract.
type PasswordResetter interface {
	MayRedeem(ctx context.Context, ip string) Decision
	Reset(ctx context.Context, tx db.Tx[db.Tenant], token, password string,
		from Client) (*Session, error)
}

// SessionReader lists a person's live sessions without exposing credentials.
// The caller owns the transaction; errors preserve the underlying command contract.
type SessionReader interface {
	Sessions(ctx context.Context, tx db.Tx[db.Tenant], userID, current uuid.UUID) ([]*SessionListing, error)
}

// SessionRevoker revokes one session owned by the supplied person, by its public reference.
// The caller owns the transaction; errors preserve the underlying command contract.
type SessionRevoker interface {
	RevokeSession(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, ref string) error
}

// AllSessionsRevoker ends every session of the supplied person, including the current one.
// The caller owns the transaction; errors preserve the underlying command contract.
type AllSessionsRevoker interface {
	RevokeAllSessions(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (int, error)
}

// RoleReader lists the caller's tenant roles in name order.
// The caller owns the transaction; errors preserve the underlying command contract.
type RoleReader interface {
	Roles(ctx context.Context, tx db.Tx[db.Tenant]) ([]*Role, error)
}

// RoleWriter writes declared role grants under the tenant administration lock.
// The caller owns the transaction; errors preserve the underlying command contract.
type RoleWriter interface {
	SetRole(ctx context.Context, tx db.Tx[db.Tenant], name string, permissions []string, declared []tenancy.Grant) (*Role, error)
}

// RoleEditor lists and writes tenant role grants.
// The caller owns the transaction; errors preserve the underlying command contract.
type RoleEditor interface {
	RoleReader
	RoleWriter
}

// RoleRepairer reads role grants and reports whether a locked role write changed them.
// The caller owns the transaction; errors preserve the underlying command contract.
type RoleRepairer interface {
	Roles(ctx context.Context, tx db.Tx[db.Tenant]) ([]*Role, error)
	SetRoleChanged(ctx context.Context, tx db.Tx[db.Tenant], name string, permissions []string,
		declared []tenancy.Grant) (*Role, bool, error)
}

// SessionAdministration lists sessions and revokes one or all except the named session.
// The caller owns the transaction; errors preserve the underlying command contract.
type SessionAdministration interface {
	Sessions(ctx context.Context, tx db.Tx[db.Tenant], userID, current uuid.UUID) ([]*SessionListing, error)
	RevokeSession(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, ref string) error
	RevokeSessions(ctx context.Context, tx db.Tx[db.Tenant], userID, except uuid.UUID) error
}

// SessionOpener opens a session for an externally verified account, preserving factor checks.
// The caller owns the transaction; errors preserve the underlying command contract.
type SessionOpener interface {
	Open(ctx context.Context, tx db.Tx[db.Tenant], user uuid.UUID, from Client, via SignInMethod) (*Session, *Identity, error)
}
