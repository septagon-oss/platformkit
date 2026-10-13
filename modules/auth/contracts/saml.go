package contracts

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
)

// SAMLProvider is one tenant's SAML 2.0 identity provider, as this module needs
// it — the value behind contracts.SAMLProviders, and the reason the ACS can be one
// handler for every customer on the installation.
//
// It names no SDK and no XML type: what this module asks its composition for is the
// three strings a SAML leg reads (who the assertions come from, who they are
// addressed to, and which attribute carries the address) and the policy that says
// what a verified address means here. `github.com/crewjam/saml` enters on the other
// side of this value, in internal, where an SDK belongs.
//
// There is no secret reference here, and so no `Secrets` lookup on this leg: an IdP's
// metadata is what that IdP publishes to anyone who asks, and what its certificate
// verifies is a signature. The service provider's *own* signing key would be a
// secret, and v1 has none — AuthnRequests go unsigned and the SP metadata carries no
// KeyDescriptor — because where that key lives, and who rotates it, is a decision
// this repository has not made yet.
type SAMLProvider struct {
	// EntityID is this installation's SAML identity for that tenant, and the
	// audience every assertion presented here has to name. It is the tenant's, not
	// the process's: one installation fronting two customers is two service
	// providers as far as any IdP is concerned.
	EntityID string `json:"entityId"`
	// MetadataURL and MetadataXML are the two sources of the IdP's EntityDescriptor.
	// Both may be set; the document then wins, and the URL is what a federation's
	// own page points at. A document kept beside the URL is the reason a metadata
	// endpoint going offline at the IdP does not close this tenant's door.
	MetadataURL string `json:"metadataUrl,omitempty"`
	MetadataXML string `json:"metadataXml,omitempty"`
	// EmailAttribute names the assertion attribute carrying the address. The NameID
	// is never used as one: the attribute is the configured contract between this
	// tenant's directory and this installation, and a subject format that changes at
	// the IdP would otherwise silently change who may sign in.
	EmailAttribute string `json:"emailAttribute"`
	// Registration is RegistrationDisabled, RegistrationExisting or
	// RegistrationProvision — the same three answers as OIDC, from the same column
	// family's rule, because what an unknown address means does not depend on which
	// protocol verified it.
	Registration string `json:"registration,omitempty"`
	// Roles is what a provisioned person is given, and only under `provision`.
	Roles []string `json:"roles,omitempty"`
}

// RegistrationMode is the mode with its default spelled out: a tenant that named
// nothing, and a row written before the choice existed, both get today's behaviour.
func (p SAMLProvider) RegistrationMode() string {
	if p.Registration == "" {
		return RegistrationExisting
	}
	return p.Registration
}

// SAMLProviders is where the resolved tenant's SAML provider comes from, and it is
// the reason one process can serve two customers who federate at two IdPs.
//
// It answers false — not an error — when this tenant signs in no other way than it
// always has, because "no SSO here" is a fact about one tenant and a 500 would tell
// a caller that nobody's identity provider is down. The transaction is the request's
// own tenant transaction, which is how the answer is per request and per host rather
// than per process — 0028's rule, and OIDCProviders' shape.
//
// Declared here, over the capability this module uses, rather than imported from the
// tenant module: the same rule OIDCProviders follows, and the one that makes a test's
// stand-in a map.
type SAMLProviders interface {
	ProviderOf(ctx context.Context, tx db.Tx[db.Tenant]) (*SAMLProvider, bool, error)
}
