package contracts

// SAMLSettings is which SAML 2.0 identity provider one tenant's people sign in
// against, and what an address that provider vouches for means here. Like
// OIDCSettings it is the same record read and written, and for the same reason:
// a command that took a narrower input than the read returns is two shapes to
// keep in step for no rule.
//
// The three identity fields are the three questions a SAML sign-in answers:
// *whose* assertion this is (the metadata's entity ID, read out of the document
// and never written twice), *who it is addressed to* (EntityID, this
// installation's SAML identity for this tenant — the audience every assertion has
// to name), and *where the address is* (EmailAttribute, the assertion attribute
// the customer's directory puts the mailbox in).
//
// MetadataXML is not a secret — an IdP's certificate and SSO location are what
// that IdP publishes to anyone who asks — but it is never copied into an event
// payload either, because a payload is copied into the audit trail and a
// multi-kilobyte XML there answers no question anybody asks of a trail.
//
// Both metadata sources may be present; the XML then wins at sign-in and the URL
// is what a federation's own page says. Neither present is the one shape that
// cannot be exercised, and it is refused at the write (and by the CHECK in
// migrations/000046, where SQL can see it).
type SAMLSettings struct {
	EntityID       string   `json:"entityId" maxLength:"512" doc:"This installation's SAML identity for that tenant — the audience every assertion must name" example:"urn:pkit:acme"`
	MetadataURL    string   `json:"metadataUrl,omitempty" maxLength:"2048" doc:"Where the IdP documents itself; https unless it is local"`
	MetadataXML    string   `json:"metadataXml,omitempty" maxLength:"65536" doc:"The IdP's EntityDescriptor, kept so a metadata URL that goes offline does not close this tenant's door; authoritative when present"`
	EmailAttribute string   `json:"emailAttribute" maxLength:"256" doc:"The assertion attribute that carries the address" example:"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"`
	Registration   string   `json:"registration,omitempty" doc:"disabled, existing or provision: what an address the provider vouches for and this tenant has no account for does"`
	Roles          []string `json:"roles,omitempty" doc:"What a provisioned person is given, and only under provision"`
}

// RegistrationMode is the mode with its default spelled out, so no caller has to
// remember that an empty column and `existing` are the same answer.
func (s SAMLSettings) RegistrationMode() string {
	if s.Registration == "" {
		return RegistrationExisting
	}
	return s.Registration
}
