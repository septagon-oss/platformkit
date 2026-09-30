package main

// The two adapters the single sign-on legs need. Both are here, in the
// composition, rather than exported from modules/auth, because each is the one
// line that has to know two modules exist. A module owns the port it declares
// and nothing about who answers it: modules/auth imports no other module's
// contracts, and this file is where the modules the application composes are
// named together.

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// oidcSource is the tenant module's read of its own provider, narrowed to the
// one method this adapter calls.
type oidcSource interface {
	OIDCOf(ctx context.Context, tx db.Tx[db.Tenant]) (*tenantcontracts.OIDCSettings, bool, error)
}

// tenantProviders answers modules/auth's OIDCProviders port from the tenant
// this request's Host resolved. The transaction is the request's own, which is
// what makes the answer per tenant and per request rather than per process.
type tenantProviders struct{ tenants oidcSource }

func (p tenantProviders) ProviderOf(ctx context.Context, tx db.Tx[db.Tenant]) (*authcontracts.OIDCProvider, bool, error) {
	settings, ok, err := p.tenants.OIDCOf(ctx, tx)
	if err != nil || !ok {
		return nil, false, err
	}
	return &authcontracts.OIDCProvider{
		Issuer: settings.Issuer, ClientID: settings.ClientID, SecretRef: settings.SecretRef,
		RedirectPath: settings.RedirectPath, Registration: settings.Registration, Roles: settings.Roles,
	}, true, nil
}

// provisioner answers modules/auth's Provisioner port over the user module, for
// the tenant whose registration mode is `provision`. Both calls run in the
// request's tenant transaction, so the person and the roles are made or none is:
// an unknown role name is refused by the user module, the transaction aborts, and
// no half-made person is left behind. The address the identity provider verified
// is what the row is made from, and the confirmation the callback records is
// what makes it able to sign in — this application hands out no password here,
// because a secret nobody chose is not a credential.
//
// Which roles the mode hands out is the tenant operator's decision, taken at
// POST /tenants/{id}/oidc and stored in the tenant's row; this line decides only
// that the roles named there and no others are the ones the person arrives with.
type provisioner struct{ users usercontracts.Service }

func (p provisioner) Provision(ctx context.Context, tx db.Tx[db.Tenant], email, displayName string, roles []string) (uuid.UUID, error) {
	person, err := p.users.Invite(ctx, tx, email, displayName)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := p.users.SetRoles(ctx, tx, person.ID, roles); err != nil {
		return uuid.Nil, err
	}
	return person.ID, nil
}
