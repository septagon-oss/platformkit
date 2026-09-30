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
