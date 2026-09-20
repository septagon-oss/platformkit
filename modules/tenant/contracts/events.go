package contracts

import (
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events/transport"
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
)

// Payloads is the type of each of those events' payload, for the manifest and
// the documents the composition projects from it. None is a rest.Spec event:
// the control plane belongs to no tenant, so there is no Spec to publish a
// Tenant row, and each event names what a subscriber would act on instead.
var Payloads = []transport.Declared{
	transport.Declare[Created](EventCreated),
	transport.Declare[Suspended](EventSuspended),
	transport.Declare[HostAdded](EventHostAdded),
}

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
