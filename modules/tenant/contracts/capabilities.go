package contracts

import (
	"context"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// HostLookup resolves an active tenant by host; unknown and suspended hosts are not found.
// The caller owns the transaction; errors preserve the underlying command contract.
type HostLookup interface {
	ByHost(ctx context.Context, tx db.Tx[db.System], host string) (tenancy.Tenant, error)
}

// HostLister lists the transaction's tenant hosts, primary first.
// The caller owns the transaction; errors preserve the underlying command contract.
type HostLister interface {
	Hosts(ctx context.Context, tx db.Tx[db.Tenant]) ([]string, error)
}

// OIDCReader reads this tenant's OIDC configuration, returning false when absent.
// The caller owns the transaction; errors preserve the underlying command contract.
type OIDCReader interface {
	OIDCOf(ctx context.Context, tx db.Tx[db.Tenant]) (*OIDCSettings, bool, error)
}

// SAMLReader reads this tenant's SAML configuration, returning false when absent.
// The caller owns the transaction; errors preserve the underlying command contract.
type SAMLReader interface {
	SAMLSettingsOf(ctx context.Context, tx db.Tx[db.Tenant]) (*SAMLSettings, bool, error)
}

// Lister lists undeleted tenants, including suspended tenants, in a system transaction.
// The caller owns the transaction; errors preserve the underlying command contract.
type Lister interface {
	List(ctx context.Context, tx db.Tx[db.System]) ([]*Tenant, error)
}

// Bootstrapper lists and creates tenants within the caller's system transaction.
// The caller owns the transaction; errors preserve the underlying command contract.
type Bootstrapper interface {
	List(ctx context.Context, tx db.Tx[db.System]) ([]*Tenant, error)
	Create(ctx context.Context, tx db.Tx[db.System], in NewTenant) (*Tenant, error)
}

// LocaleSetter sets a tenant's supported locale choices in a system transaction.
// The caller owns the transaction; errors preserve the underlying command contract.
type LocaleSetter interface {
	SetLocale(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in SetLocale) (*Tenant, error)
}

// Directory lists and reads tenants in the control plane.
// The caller owns the transaction; errors preserve the underlying command contract.
type Directory interface {
	List(ctx context.Context, tx db.Tx[db.System]) ([]*Tenant, error)
	Get(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*Tenant, error)
}

// Creator atomically creates a tenant, its first host and hook contributions.
// The caller owns the transaction; errors preserve the underlying command contract.
type Creator interface {
	Create(ctx context.Context, tx db.Tx[db.System], in NewTenant) (*Tenant, error)
}

// Lifecycle suspends, resumes, renames and retires tenants through their guarded commands.
// The caller owns the transaction; errors preserve the underlying command contract.
type Lifecycle interface {
	Suspend(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*Tenant, error)
	Reactivate(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*Tenant, error)
	Rename(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in Rename) (*Tenant, error)
	Delete(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in Delete) (*Tenant, error)
}

// HostManager adds or removes hosts while preserving the primary and last-host rules.
// The caller owns the transaction; errors preserve the underlying command contract.
type HostManager interface {
	AddHost(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, host string, primary bool) (*Tenant, error)
	RemoveHost(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, host string) (*Tenant, error)
}

// OIDCWriter sets or clears OIDC configuration without revoking existing sessions.
// The caller owns the transaction; errors preserve the underlying command contract.
type OIDCWriter interface {
	SetOIDC(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in OIDCSettings) (*Tenant, error)
	ClearOIDC(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*Tenant, error)
}

// SAMLWriter sets or clears SAML configuration independently of OIDC.
// The caller owns the transaction; errors preserve the underlying command contract.
type SAMLWriter interface {
	SetSAML(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in SAMLSettings) (*Tenant, error)
	ClearSAML(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*Tenant, error)
}
