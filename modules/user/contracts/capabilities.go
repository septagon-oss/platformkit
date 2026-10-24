package contracts

import (
	"context"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
)

// Lookup reads one visible person in the caller's tenant; an absent row is not found.
// The caller owns the transaction; errors preserve the underlying command contract.
type Lookup interface {
	Get(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*User, error)
}

// RetainedLookup reads a person including retained, soft-deleted rows in the caller's tenant.
// The caller owns the transaction; errors preserve the underlying command contract.
type RetainedLookup interface {
	Retained(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*User, error)
}

// SystemProvisioner creates a person in an explicit target tenant within a system transaction.
// The caller owns the transaction; errors preserve the underlying command contract.
type SystemProvisioner interface {
	Provision(ctx context.Context, tx db.Tx[db.System], tenantID uuid.UUID, email, displayName, password string, roles []string) (*User, error)
}

// InvitationWriter invites a person and assigns roles in the caller's transaction.
// The caller owns the transaction; errors preserve the underlying command contract.
type InvitationWriter interface {
	Invite(ctx context.Context, tx db.Tx[db.Tenant], email, displayName string) (*User, error)
	SetRoles(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, roles []string) (*User, error)
}

// RegistrationUsers looks up and invites accounts with trusted registration roles.
// The caller owns the transaction; errors preserve the underlying command contract.
type RegistrationUsers interface {
	ByEmail(ctx context.Context, tx db.Tx[db.Tenant], email string) (*User, error)
	Invite(ctx context.Context, tx db.Tx[db.Tenant], email, displayName string) (*User, error)
	SetRoles(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, roles []string) (*User, error)
}

// EmailRegistrar creates unverified accounts and activates them after mailbox proof.
// The caller owns the transaction; errors preserve the underlying command contract.
type EmailRegistrar interface {
	RegisterUnverified(context.Context, db.Tx[db.Tenant], PasswordRegistration) (*User, error)
	VerifyEmail(context.Context, db.Tx[db.Tenant], uuid.UUID, string) (*User, error)
}

// PendingRegistrar creates accounts that require approval before activation.
// The caller owns the transaction; errors preserve the underlying command contract.
type PendingRegistrar interface {
	RegisterPending(context.Context, db.Tx[db.Tenant], PendingRegistration) (*User, error)
}

// Provisioning reconciles tenant-local accounts through their lifecycle commands.
// The caller owns the transaction; errors preserve the underlying command contract.
type Provisioning interface {
	Invite(ctx context.Context, tx db.Tx[db.Tenant], email, displayName string) (*User, error)
	ConfirmAddress(context.Context, db.Tx[db.Tenant], uuid.UUID, string) (*User, error)
	Deactivate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*User, error)
	Reactivate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*User, error)
	Get(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*User, error)
	SetRoles(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, roles []string) (*User, error)
}

// RoleHolders lists active holders of the named roles; an empty set selects nobody.
// The caller owns the transaction; errors preserve the underlying command contract.
type RoleHolders interface {
	Holders(ctx context.Context, tx db.Tx[db.Tenant], roles []string) ([]uuid.UUID, error)
}

// AddressLookup finds a tenant-local person by case-insensitive email address.
// The caller owns the transaction; errors preserve the underlying command contract.
type AddressLookup interface {
	ByEmail(ctx context.Context, tx db.Tx[db.Tenant], email string) (*User, error)
}

// SeedWriter looks up, invites and configures seed accounts in one tenant transaction.
// The caller owns the transaction; errors preserve the underlying command contract.
type SeedWriter interface {
	ByEmail(ctx context.Context, tx db.Tx[db.Tenant], email string) (*User, error)
	Get(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*User, error)
	Invite(ctx context.Context, tx db.Tx[db.Tenant], email, displayName string) (*User, error)
	SetRoles(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, roles []string) (*User, error)
	SetPassword(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, password string) error
}

// RegistrationApprover activates pending accounts, recording the supplied actor.
// The caller owns the transaction; errors preserve the underlying command contract.
type RegistrationApprover interface {
	ApproveRegistration(context.Context, db.Tx[db.Tenant], uuid.UUID, uuid.UUID) (*User, error)
}

// PasswordWriter sets a password and reads the resulting tenant-local account.
// The caller owns the transaction; errors preserve the underlying command contract.
type PasswordWriter interface {
	SetPassword(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, password string) error
	Get(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*User, error)
}

// RoleSetter replaces a person's roles; an unchanged set writes nothing.
// The caller owns the transaction; errors preserve the underlying command contract.
type RoleSetter interface {
	SetRoles(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, roles []string) (*User, error)
}

// HandleSetter claims or changes a tenant-local handle; a taken name conflicts.
// The caller owns the transaction; errors preserve the underlying command contract.
type HandleSetter interface {
	SetHandle(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, handle string) (*User, error)
}

// Deactivator stands down an account while preserving the administration floor.
// The caller owns the transaction; errors preserve the underlying command contract.
type Deactivator interface {
	Deactivate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*User, error)
}
