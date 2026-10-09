package internal

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// samlRow is the six columns migrations/000046 added to tenants, read and written
// by name. It is its own struct for oidcRow's reason, which transfers whole: the
// tenant entity travels, and a provider nobody configured would appear in every
// copy of it as five empty strings rather than as the absence it is.
type samlRow struct {
	EntityID       *string        `gorm:"column:saml_entity_id"`
	MetadataURL    *string        `gorm:"column:saml_metadata_url"`
	MetadataXML    *string        `gorm:"column:saml_metadata_xml"`
	EmailAttribute *string        `gorm:"column:saml_email_attribute"`
	Registration   string         `gorm:"column:saml_registration"`
	Roles          pq.StringArray `gorm:"column:saml_roles;type:text[]"`
}

// TableName pins the read onto tenants, which this row type covers six columns of
// and no more.
func (samlRow) TableName() string { return "tenants" }

// samlColumns is the six names, spelled once, so the control-plane read and the
// sign-in's read cannot select different halves of a provider.
const samlColumns = "saml_entity_id, saml_metadata_url, saml_metadata_xml, saml_email_attribute, saml_registration, saml_roles"

func (r samlRow) settings() (contracts.SAMLSettings, bool) {
	if r.EntityID == nil || *r.EntityID == "" {
		return contracts.SAMLSettings{}, false
	}
	return contracts.SAMLSettings{
		EntityID: *r.EntityID, MetadataURL: deref(r.MetadataURL), MetadataXML: deref(r.MetadataXML),
		EmailAttribute: deref(r.EmailAttribute), Registration: r.Registration, Roles: []string(r.Roles),
	}, true
}

// SetSAML says which SAML identity provider one tenant's people sign in against.
//
// The refusals are migrations/000046's shape rule spelled in Go for the parts SQL
// cannot see — that an entity ID is a URI rather than a sentence, that a metadata
// document is an identity provider's and carries a certificate — each naming the
// field an operator has to fix. Writing the same values again changes nothing and
// publishes nothing, for oidc.SetOIDC's reason: a retry must not read as two
// changes in an audit trail.
//
// Nothing is dialled here. A metadata URL that does not resolve reads as the 503
// sign-in already answers for a wrong issuer, because this repository has no
// precedent for the control plane dialling an address a caller named — and a
// tenant whose IdP is having a bad morning has a provider, not none.
func (s *Service) SetSAML(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in contracts.SAMLSettings) (*contracts.Tenant, error) {
	if err := validSAML(in); err != nil {
		return nil, err
	}
	t, err := s.Get(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	before, _ := s.samlOfSystem(tx, id)
	if sameSAML(before, in) {
		return t, nil
	}
	err = tx.DB().Table("tenants").Where("id = ?", id).Updates(map[string]any{
		"saml_entity_id": in.EntityID, "saml_metadata_url": in.MetadataURL,
		"saml_metadata_xml": in.MetadataXML, "saml_email_attribute": in.EmailAttribute,
		"saml_registration": samlMode(in), "saml_roles": samlRoles(in), "updated_at": db.Now(),
	}).Error
	if err != nil {
		return nil, crud.Classify(err)
	}
	if err := events.PublishFor(ctx, tx, id, contracts.EventSAMLSet, contracts.SAMLSet{
		TenantID: id, EntityID: in.EntityID, MetadataURL: in.MetadataURL,
		EmailAttribute: in.EmailAttribute, Registration: samlMode(in), Roles: in.Roles,
		WasEntityID: before.EntityID, Replaced: before.EntityID != "", At: db.Now(),
	}); err != nil {
		return nil, err
	}
	return s.Get(ctx, tx, id)
}

// ClearSAML takes a tenant's SAML provider away. Every column goes back to the
// state the CHECK reads as "no provider here", in one write, and the OIDC columns
// beside them are not named: a tenant that runs both protocols today, and drops
// one, keeps the other.
func (s *Service) ClearSAML(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*contracts.Tenant, error) {
	t, err := s.Get(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	before, ok := s.samlOfSystem(tx, id)
	if !ok {
		return t, nil
	}
	err = tx.DB().Table("tenants").Where("id = ?", id).Updates(map[string]any{
		"saml_entity_id": nil, "saml_metadata_url": nil, "saml_metadata_xml": nil,
		"saml_email_attribute": nil, "saml_registration": contracts.RegistrationExisting,
		"saml_roles": pq.StringArray{}, "updated_at": db.Now(),
	}).Error
	if err != nil {
		return nil, crud.Classify(err)
	}
	return t, events.PublishFor(ctx, tx, id, contracts.EventSAMLCleared, contracts.SAMLCleared{
		TenantID: id, EntityID: before.EntityID, At: db.Now(),
	})
}

// SAMLSettingsOf is the sign-in's read: this tenant's SAML provider, in the
// transaction the Host header resolved, which is the whole of why two tenants on
// one process are verified by two IdPs against one ACS URL each.
func (s *Service) SAMLSettingsOf(_ context.Context, tx db.Tx[db.Tenant]) (*contracts.SAMLSettings, bool, error) {
	var row samlRow
	err := tx.DB().Model(samlRow{}).Where("id = ?", db.TenantOf(tx).ID).Select(samlColumns).Take(&row).Error
	if err != nil {
		return nil, false, crud.Classify(err)
	}
	settings, ok := row.settings()
	if !ok {
		return nil, false, nil
	}
	return &settings, true, nil
}

// samlOfSystem is the same read from the control plane, for the "did anything
// change" comparison and for the entity ID the event carries as the one before.
func (s *Service) samlOfSystem(tx db.Tx[db.System], id uuid.UUID) (contracts.SAMLSettings, bool) {
	var row samlRow
	err := tx.DB().Model(samlRow{}).Where("id = ?", id).Select(samlColumns).Take(&row).Error
	if err != nil {
		return contracts.SAMLSettings{}, false
	}
	return row.settings()
}

// sameSAML is "nothing changed", written once, with the mode through its default
// and the roles element by element — the two lists and one row that mean the same
// tenant must not read as a change to the trail.
func sameSAML(a, b contracts.SAMLSettings) bool {
	return a.EntityID == b.EntityID && a.MetadataURL == b.MetadataURL &&
		a.MetadataXML == b.MetadataXML && a.EmailAttribute == b.EmailAttribute &&
		samlMode(a) == samlMode(b) && slices.Equal(a.Roles, b.Roles)
}

// samlMode is the mode with its default spelled out, so a row written without one
// says `existing` rather than "" and the CHECK has one thing to check.
func samlMode(in contracts.SAMLSettings) string { return in.RegistrationMode() }

// samlRoles is `saml_roles` spelled as the column is: a NOT NULL text[] whose
// empty value is `{}`. pq turns a nil slice into a NULL the column refuses, and
// "no roles to hand out" is the ordinary answer for a tenant registered as
// `existing`, so the empty case is the one that has to be right.
func samlRoles(in contracts.SAMLSettings) pq.StringArray {
	if in.Roles == nil {
		return pq.StringArray{}
	}
	return pq.StringArray(in.Roles)
}

// validSAML is the write's rule, and every refusal in it is correctable by the
// operator who wrote it.
//
// An entity ID is an absolute URI with a scheme and no query: it is a name, not a
// location, and `urn:` is a legitimate scheme for one — refusing it would refuse
// the product, not the mistake. A metadata URL follows the issuer's rule, https
// unless the host is local, because a document that chooses who may sign people in
// is not worth less protection than the client secret beside it. A metadata
// document has to parse as an identity provider's, offer a Redirect or POST SSO
// location, and carry a signing certificate: without one no assertion could ever
// verify, and a provider that can only ever answer 403 is a configuration nobody
// should be able to save.
//
// Metadata signatures are not verified here. Most federation metadata ships
// unsigned, and the certificate this document carries is what verification
// consumes either way; requiring a signature would refuse the common case to
// protect a document whose integrity the sign-in checks assertion by assertion
// already.
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
		if err := validIdPMetadata(in.MetadataXML); err != nil {
			return err
		}
	}
	switch samlMode(in) {
	case contracts.RegistrationDisabled, contracts.RegistrationExisting, contracts.RegistrationProvision:
	default:
		return fmt.Errorf("%w: saml.registration %q is not disabled, existing or provision", crud.ErrInvalid, in.Registration)
	}
	if samlMode(in) == contracts.RegistrationProvision && len(in.Roles) == 0 {
		return fmt.Errorf("%w: provision with no roles would make people who can do nothing", crud.ErrInvalid)
	}
	for _, role := range in.Roles {
		if strings.TrimSpace(role) == "" {
			return fmt.Errorf("%w: saml.roles carries an empty name", crud.ErrInvalid)
		}
	}
	return nil
}

// validIdPMetadata is the metadata document's rule: the library's parse, which
// runs the XML round-trip validator against an entity-expansion bomb, and then the
// three things a document has to be for a sign-in to be possible against it.
//
// Both bindings are accepted because an IdP that offers only the Redirect binding
// is the common case and one that offers only POST is legitimate; a document that
// offers neither could not carry an assertion here at all.
func validIdPMetadata(xml string) error {
	descriptor, err := samlsp.ParseMetadata([]byte(xml))
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
	if signingCertificate(idp.KeyDescriptors) == "" {
		return fmt.Errorf("%w: saml.metadataXml carries no signing certificate, so no assertion it vouches for could ever be verified", crud.ErrInvalid)
	}
	return nil
}

// signingCertificate is the document's own words for "the certificate an
// assertion's signature is checked against": a KeyDescriptor whose use is signing
// — or unqualified, which the schema reads as both — carrying an X509 certificate.
func signingCertificate(keys []saml.KeyDescriptor) string {
	for _, key := range keys {
		if key.Use != "" && key.Use != "signing" {
			continue
		}
		for _, cert := range key.KeyInfo.X509Data.X509Certificates {
			if cert.Data != "" {
				return cert.Data
			}
		}
	}
	return ""
}
