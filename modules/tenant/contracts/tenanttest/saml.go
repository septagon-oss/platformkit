package tenanttest

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// SetSAML mirrors internal.Service.SetSAML, refusals included, for the reason
// validOIDC's comment gives: which provider a tenant's people sign in against is a
// rule about the values and not about the table they land in, so the fake refuses
// the same half-providers, the same unparsable document, the same certificate-less
// IdP, in the same words. A fake that let through what the SQL service refuses
// would let a consumer's test pass against the double and fail against the
// database, which is the pair these two files exist to make impossible.
func (f *Fake) SetSAML(ctx context.Context, _ db.Tx[db.System], id uuid.UUID, in contracts.SAMLSettings) (*contracts.Tenant, error) {
	if err := validSAML(in); err != nil {
		return nil, err
	}
	f.mu.Lock()
	t, ok := f.live(id)
	if !ok {
		f.mu.Unlock()
		return nil, crud.ErrNotFound
	}
	changed := !sameSAML(f.saml[id], in)
	if f.saml == nil {
		f.saml = map[uuid.UUID]contracts.SAMLSettings{}
	}
	in.Registration = in.RegistrationMode()
	f.saml[id] = in
	t.UpdatedAt = db.Now()
	f.tenants[id] = t
	f.mu.Unlock()
	if changed {
		f.publish(ctx, id, contracts.EventSAMLSet)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(id)
}

// ClearSAML mirrors internal.Service.ClearSAML, including the two rules that make
// the pair independent: clearing SAML leaves the tenant's OIDC provider in the map
// untouched, and clearing it twice changes nothing and publishes nothing.
func (f *Fake) ClearSAML(ctx context.Context, _ db.Tx[db.System], id uuid.UUID) (*contracts.Tenant, error) {
	f.mu.Lock()
	t, ok := f.live(id)
	if !ok {
		f.mu.Unlock()
		return nil, crud.ErrNotFound
	}
	_, had := f.saml[id]
	delete(f.saml, id)
	t.UpdatedAt = db.Now()
	f.tenants[id] = t
	f.mu.Unlock()
	if had {
		f.publish(ctx, id, contracts.EventSAMLCleared)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(id)
}

// SAMLSettingsOf mirrors internal.Service.SAMLSettingsOf, answered from the tenant
// the transaction carries, and answers false — not an error — for the tenant with
// no SAML provider, including the tenant that does have an OIDC one.
func (f *Fake) SAMLSettingsOf(_ context.Context, tx db.Tx[db.Tenant]) (*contracts.SAMLSettings, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	settings, ok := f.saml[db.TenantOf(tx).ID]
	if !ok {
		return nil, false, nil
	}
	return &settings, true, nil
}

// sameSAML and validSAML are internal's helpers, kept here rather than shared for
// the reason mode, sameOIDC and validOIDC are: a fake that imported the
// implementation would stop being a second opinion about it. The metadata rule
// calls the same library entry point the implementation calls, so the two can
// disagree about a check but not about what a metadata document is.
func sameSAML(a, b contracts.SAMLSettings) bool {
	return a.EntityID == b.EntityID && a.MetadataURL == b.MetadataURL &&
		a.MetadataXML == b.MetadataXML && a.EmailAttribute == b.EmailAttribute &&
		a.RegistrationMode() == b.RegistrationMode() && slices.Equal(a.Roles, b.Roles)
}

func validSAML(in contracts.SAMLSettings) error {
	u, err := url.Parse(in.EntityID)
	switch {
	case err != nil || !u.IsAbs() || u.Host == "" && u.Scheme != "urn":
		return fmt.Errorf("%w: saml.entityId %q is not an absolute URI", crud.ErrInvalid, in.EntityID)
	case u.RawQuery != "" || u.Fragment != "":
		return fmt.Errorf("%w: saml.entityId %q carries a query or fragment; an entity ID is a name", crud.ErrInvalid, in.EntityID)
	case in.EmailAttribute == "" || strings.TrimSpace(in.EmailAttribute) != in.EmailAttribute:
		return fmt.Errorf("%w: saml.emailAttribute is empty or padded: name the attribute the assertion carries the address in", crud.ErrInvalid)
	case in.MetadataURL == "" && in.MetadataXML == "":
		return fmt.Errorf("%w: saml needs a metadata source: a URL, a document, or both", crud.ErrInvalid)
	}
	if in.MetadataURL != "" {
		m, err := url.Parse(in.MetadataURL)
		switch {
		case err != nil || m.Host == "":
			return fmt.Errorf("%w: saml.metadataUrl %q is not a URL", crud.ErrInvalid, in.MetadataURL)
		case m.Scheme != "https" && !config.Local(m.Host):
			return fmt.Errorf("%w: saml.metadataUrl %q is not https", crud.ErrInvalid, in.MetadataURL)
		case m.Fragment != "":
			return fmt.Errorf("%w: saml.metadataUrl %q carries a fragment", crud.ErrInvalid, in.MetadataURL)
		}
	}
	if in.MetadataXML != "" {
		descriptor, err := samlsp.ParseMetadata([]byte(in.MetadataXML))
		if err != nil {
			return fmt.Errorf("%w: saml.metadataXml does not parse as SAML metadata: %v", crud.ErrInvalid, err)
		}
		if len(descriptor.IDPSSODescriptors) == 0 {
			return fmt.Errorf("%w: saml.metadataXml names no identity provider: an EntityDescriptor with only a service provider role cannot vouch for anybody", crud.ErrInvalid)
		}
		idp := descriptor.IDPSSODescriptors[0]
		sso := false
		for _, endpoint := range idp.SingleSignOnServices {
			if endpoint.Location != "" && (endpoint.Binding == saml.HTTPRedirectBinding || endpoint.Binding == saml.HTTPPostBinding) {
				sso = true
			}
		}
		if !sso {
			return fmt.Errorf("%w: saml.metadataXml offers no Redirect or POST single sign-on location", crud.ErrInvalid)
		}
		signed := false
		for _, key := range idp.KeyDescriptors {
			if key.Use != "" && key.Use != "signing" {
				continue
			}
			for _, cert := range key.KeyInfo.X509Data.X509Certificates {
				if cert.Data != "" {
					signed = true
				}
			}
		}
		if !signed {
			return fmt.Errorf("%w: saml.metadataXml carries no signing certificate, so no assertion it vouches for could ever be verified", crud.ErrInvalid)
		}
	}
	switch in.RegistrationMode() {
	case contracts.RegistrationDisabled, contracts.RegistrationExisting, contracts.RegistrationProvision:
	default:
		return fmt.Errorf("%w: saml.registration %q is not disabled, existing or provision", crud.ErrInvalid, in.Registration)
	}
	if in.RegistrationMode() == contracts.RegistrationProvision && len(in.Roles) == 0 {
		return fmt.Errorf("%w: provision with no roles would make people who can do nothing", crud.ErrInvalid)
	}
	for _, role := range in.Roles {
		if strings.TrimSpace(role) == "" {
			return fmt.Errorf("%w: saml.roles carries an empty name", crud.ErrInvalid)
		}
	}
	return nil
}
