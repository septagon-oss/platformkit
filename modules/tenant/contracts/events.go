package contracts

import (
	"time"

	"github.com/google/uuid"
)

// The events this module emits. They are written from the transaction that
// changed the control plane, which belongs to no tenant, so they go through
// events.PublishFor rather than events.Publish — the one place in the application
// where the tenant an event belongs to is an argument.
//
// A subscriber names one of these constants rather than a string, so renaming
// an event is a compile error in every module that listens for it.
//
// Every lifecycle verb has one of these, and every one of them is accompanied by
// EventLifecycleRecorded written in the *operator's* scope rather than the
// subject's — the same fact, in the trail of the installation that caused it and
// in the trail of the customer it happened to. See that event's own comment for
// why it is a name of its own.
const (
	EventCreated     = "tenant.created"
	EventSuspended   = "tenant.suspended"
	EventHostAdded   = "tenant.host_added"
	EventLocaleSet   = "tenant.locale_set"
	EventRenamed     = "tenant.renamed"
	EventHostRemoved = "tenant.host_removed"
	EventReactivated = "tenant.reactivated"
	EventDeleted     = "tenant.deleted"

	// EventLifecycleRecorded is the operator's copy of a lifecycle verb: the one
	// row the installation keeps of what it did to one of its customers, in the
	// installation's own trail.
	//
	// It is a separate name rather than the verb event published twice because two
	// rows with one name in two tenants are indistinguishable to a subscriber, and
	// a subscriber that *acts* on an event would then act twice. The verb event
	// says what happened to a customer; this one says which verb the operator
	// performed, which is the question an audit of the control plane asks.
	EventLifecycleRecorded = "tenant.lifecycle_recorded"

	// EventOIDCSet says which provider a tenant's people sign in against changed,
	// or that there is one now. It carries the issuer and client id it replaced
	// for the same reason user.roles_set carries what a role used to grant: the
	// question an operator asks afterwards is not "did it change" but "what did
	// it change from", and a trail that answers only the first is a trail that
	// gets asked twice.
	//
	// It carries the secret *reference* and never a secret: a payload is copied
	// into audit_events, which is modules/audit's whole design.
	EventOIDCSet = "tenant.oidc_set"
	// EventOIDCCleared says a tenant has no provider now: its people sign in with
	// a password or not at all, and its /oidc/start answers 404.
	EventOIDCCleared = "tenant.oidc_cleared"

	// EventSAMLSet says a tenant's SAML 2.0 provider changed, or that there is one
	// now. It is the sibling of EventOIDCSet and carries what that one does not:
	// no secret reference, because an IdP's metadata contains no secret, and no
	// metadata document either — an outbox payload is copied into audit_events, and
	// the question the trail answers is which provider was replaced, which the
	// entity ID answers in one line rather than sixty.
	EventSAMLSet = "tenant.saml_set"
	// EventSAMLCleared says a tenant has no SAML provider now: its /saml/start
	// answers 404 and its people sign in the way they did before this provider —
	// with a password, or at an OIDC issuer, or not at all.
	EventSAMLCleared = "tenant.saml_cleared"
)

// Created is the payload of EventCreated: there is a new customer.
type Created struct {
	TenantID uuid.UUID `json:"tenantId"`
	Slug     string    `json:"slug"`
	Name     string    `json:"name"`
	Host     string    `json:"host"`
	At       time.Time `json:"at"`
}

// Suspended is the payload of EventSuspended: the tenant stopped being served.
// A subscriber that has to stop doing work for a customer reads this one.
type Suspended struct {
	TenantID uuid.UUID `json:"tenantId"`
	Slug     string    `json:"slug"`
	At       time.Time `json:"at"`
}

// HostAdded is the payload of EventHostAdded: another name resolves here.
// Primary says it is the name this tenant's absolute URLs are built on, which
// is a different fact and the one a subscriber would act on — and it is the only
// fact carried when the name was already here and somebody promoted it, which is
// the same move of the routing table seen from the other end.
type HostAdded struct {
	TenantID uuid.UUID `json:"tenantId"`
	Host     string    `json:"host"`
	Primary  bool      `json:"primary"`
	At       time.Time `json:"at"`
}

// OIDCSet is the payload of EventOIDCSet.
type OIDCSet struct {
	TenantID     uuid.UUID `json:"tenantId"`
	Issuer       string    `json:"issuer"`
	ClientID     string    `json:"clientId"`
	SecretRef    string    `json:"secretRef"`
	RedirectPath string    `json:"redirectPath,omitempty"`
	Registration string    `json:"registration"`
	Roles        []string  `json:"roles,omitempty"`
	// WasIssuer and WasClientID name the provider this one replaced, and Replaced
	// says whether there was one: the difference between a tenant being given
	// single sign-on and being switched from under it is the difference a person
	// reading the trail cares about, and it is not recoverable afterwards.
	WasIssuer string    `json:"wasIssuer,omitempty"`
	WasClient string    `json:"wasClientId,omitempty"`
	Replaced  bool      `json:"replaced,omitempty"`
	At        time.Time `json:"at"`
}

// OIDCCleared is the payload of EventOIDCCleared: the provider it names is gone.
// It names which one, because "who took our sign-in away and from when" is the
// question this event exists to answer.
type OIDCCleared struct {
	TenantID uuid.UUID `json:"tenantId"`
	Issuer   string    `json:"issuer"`
	ClientID string    `json:"clientId,omitempty"`
	At       time.Time `json:"at"`
}

// SAMLSet is the payload of EventSAMLSet.
type SAMLSet struct {
	TenantID       uuid.UUID `json:"tenantId"`
	EntityID       string    `json:"entityId"`
	MetadataURL    string    `json:"metadataUrl,omitempty"`
	EmailAttribute string    `json:"emailAttribute"`
	Registration   string    `json:"registration"`
	Roles          []string  `json:"roles,omitempty"`
	// WasEntityID names the provider this one replaced, and Replaced says whether
	// there was one: "were we moved onto a new IdP, or onto an IdP for the first
	// time" is the question a person reading this trail at 3 a.m. asks, and it is
	// not recoverable afterwards.
	WasEntityID string    `json:"wasEntityId,omitempty"`
	Replaced    bool      `json:"replaced,omitempty"`
	At          time.Time `json:"at"`
}

// SAMLCleared is the payload of EventSAMLCleared: the provider it names is gone.
// It names which one, because "who took our sign-in away and from when" is the
// question this event exists to answer.
type SAMLCleared struct {
	TenantID uuid.UUID `json:"tenantId"`
	EntityID string    `json:"entityId"`
	At       time.Time `json:"at"`
}

// LocaleSet is the payload of EventLocaleSet: the languages this tenant is served
// in changed. The whole list travels rather than the difference, because what a
// subscriber can do about it is throw away whatever it built per language — a
// search index's analysed copy, a rendered page it kept — and that question is
// asked of the set, not of one entry in it.
type LocaleSet struct {
	TenantID  uuid.UUID `json:"tenantId"`
	Default   string    `json:"default"`
	Supported []string  `json:"supported"`
	At        time.Time `json:"at"`
}

// Renamed is the payload of EventRenamed: what a tenant is called changed. Both
// names travel, because the trail's question is what it was before, and a
// payload that carried only the new one could not answer it. The slug is not
// here: it did not move, and cannot.
type Renamed struct {
	TenantID uuid.UUID `json:"tenantId"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	At       time.Time `json:"at"`
}

// HostRemoved is the payload of EventHostRemoved: one name stopped resolving
// here. A subscriber that cached something keyed on that host — a session, a
// CDN entry, a link it means to send — reads this one and stops.
type HostRemoved struct {
	TenantID uuid.UUID `json:"tenantId"`
	Host     string    `json:"host"`
	At       time.Time `json:"at"`
}

// Reactivated is the payload of EventReactivated: the tenant is served again. A
// suspended tenant stopped being served and its rows stayed, so this is the
// event a subscriber that queued work for it resumes on.
type Reactivated struct {
	TenantID uuid.UUID `json:"tenantId"`
	Slug     string    `json:"slug"`
	At       time.Time `json:"at"`
}

// Deleted is the payload of EventDeleted: the tenant was retired. Its rows are
// where they were and the trail keeps them, which is why this is a soft
// deletion's event and not a purge's: the payload of a purge would be the last
// thing anybody had of a customer.
//
// Hosts are the names the delete released, as Created names the one it attached.
// It is the same pair at either end of a customer: `tenant_hosts` is the routing
// table and a retired tenant cannot hold names it does not serve, so after this
// write the routing table no longer says which customer answered at those
// addresses — this row is the audit record that it once did, and of when that
// ended.
type Deleted struct {
	TenantID uuid.UUID `json:"tenantId"`
	Slug     string    `json:"slug"`
	Hosts    []string  `json:"hosts"`
	At       time.Time `json:"at"`
}

// LifecycleRecorded is the payload of EventLifecycleRecorded, and the payload of
// an audit row it is: identifiers and the verb, no content. Verb is the control
// plane's own word for what happened — "create", "rename", "add-host",
// "remove-host", "suspend", "reactivate", "delete" — and it is a string rather
// than an enum because an event carries identifiers, not a taxonomy its
// subscriber has to be taught. Slug and TenantID name the customer from either
// side of the boundary: the operator's trail is the one place a retired
// tenant's own trail stops being readable.
type LifecycleRecorded struct {
	Verb     string    `json:"verb"`
	TenantID uuid.UUID `json:"tenantId"`
	Slug     string    `json:"slug"`
	At       time.Time `json:"at"`
}
