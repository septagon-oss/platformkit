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
