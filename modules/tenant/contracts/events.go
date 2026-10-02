package contracts

import (
	"time"

	"github.com/google/uuid"
)

// The three events this module emits. They are written from the transaction
// that changed the control plane, which belongs to no tenant, so they go
// through events.PublishFor rather than events.Publish — the one place in the
// application where the tenant an event belongs to is an argument.
//
// A subscriber names one of these constants rather than a string, so renaming
// an event is a compile error in every module that listens for it.
const (
	EventCreated   = "tenant.created"
	EventSuspended = "tenant.suspended"
	EventHostAdded = "tenant.host_added"
	EventLocaleSet = "tenant.locale_set"
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
// Primary says it also became the name this tenant's absolute URLs are built
// on, which is a different fact and the one a subscriber would act on.
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
