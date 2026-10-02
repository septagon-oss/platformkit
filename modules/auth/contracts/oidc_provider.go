package contracts

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// Which single sign-on a tenant's people may use, and what an address the
// identity provider vouches for but this tenant has no account for does.
//
// The three values are the three answers a deployment gives, and every tenant
// answers for itself: `existing` is what this module did before the choice
// existed and is the column's default, so a tenant nobody configured behaves as
// it did yesterday. `provision` is the operator saying that an address the
// provider verified is on its own an account here, and it is refused at the
// write unless roles are named with it — a tenant that provisions people who
// hold nothing is a tenant opening a door into an empty room.
const (
	RegistrationDisabled  = "disabled"
	RegistrationExisting  = "existing"
	RegistrationProvision = "provision"
)

// OIDCProvider is one tenant's identity provider, as this module needs it.
//
// It names no SDK, no provider library and no HTTP client: what this module
// asks its composition for is the four strings that build an authorization
// request and the policy that says what a verified address means here. It is a
// value and not an interface because the thing that varies between tenants is
// the facts, and the mechanism that uses them is one.
type OIDCProvider struct {
	Issuer       string `json:"issuer"`
	ClientID     string `json:"clientId"`
	SecretRef    string `json:"secretRef"`
	RedirectPath string `json:"redirectPath,omitempty"`
	// Registration is one of the three above. An empty string is `existing`,
	// which is the default rather than a fourth state.
	Registration string `json:"registration,omitempty"`
	// Roles is what a provisioned person is given, and only under `provision`.
	Roles []string `json:"roles,omitempty"`
}

// RegistrationMode is the mode with its default spelled out, so no caller has
// to remember that an empty column and `existing` are the same answer.
func (p OIDCProvider) RegistrationMode() string {
	if p.Registration == "" {
		return RegistrationExisting
	}
	return p.Registration
}

// OIDCProviders is where the resolved tenant's provider comes from.
//
// It answers false — not an error — when this tenant has no single sign-on,
// because "no IdP here" is a fact about one tenant and a 500 would tell a
// caller that nobody's provider is down. The transaction is the request's own
// tenant transaction, which is how the read resolves the tenant the Host header
// chose rather than the one the process was configured with: the answer is per
// request, and 0028's shared-instance mode has nothing per process to unpick.
//
// Declared here, over the capability this module uses, rather than imported
// from the tenant module: the same rule Inviter and RecipientLookup follow, and
// it is what makes a test's stand-in a map.
type OIDCProviders interface {
	ProviderOf(ctx context.Context, tx db.Tx[db.Tenant]) (*OIDCProvider, bool, error)
}

// Secrets resolves a reference a row holds into the secret it names.
//
// The reference is what is in the database, in the outbox and in the audit
// trail; the secret is in the environment and in no row. That split is the
// whole of why this is a port: the composition knows whether a secret comes
// from the environment, a vault or a file, and a module that knew would know
// the deployment.
type Secrets interface {
	Lookup(ctx context.Context, ref string) (string, bool)
}

// Provisioner makes the person a verified-but-unknown id token names, where the
// tenant's registration mode is `provision`. The composition answers it over the
// user module; this module never creates a person itself, because who may exist
// is not this module's decision — it is the user module's, with its own events
// and its own floors.
type Provisioner interface {
	Provision(ctx context.Context, tx db.Tx[db.Tenant], email, displayName string, roles []string) (uuid.UUID, error)
}
