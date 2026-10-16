package contracts

import (
	"time"

	"github.com/google/uuid"
)

// The base events this module emits. The first three are kit/rest's, published
// by the Spec module.go mounts; the last four are lifecycle commands. Pending
// registration and approval events are declared in registration.go. All are
// listed in the manifest, and kit/app refuses to start if a route would publish
// one that is not.
const (
	EventCreated = "user.user.created"
	EventUpdated = "user.user.updated"
	EventDeleted = "user.user.deleted"

	EventInvited     = "user.invited"
	EventPasswordSet = "user.password_set"
	EventRolesSet    = "user.roles_set"
	EventHandleSet   = "user.handle_set"
	EventDeactivated = "user.deactivated"
	// EventAdministrationRefused is a write this module refused because it would
	// have left the tenant with nobody who can sign in and administer it. It is
	// the record of an attempt that did not happen, which is the only reason it
	// exists: the write it describes was rolled back, so nothing else about the
	// attempt is written down anywhere. See AdministrationRefused and internal.floor.
	EventAdministrationRefused = "user.administration_refused"
)

// Invited is the payload of EventInvited: a user account now exists in this
// tenant. It is published by Invite and by the bootstrap's Provision alike —
// the fact is that somebody can now be in this tenant, and Status says whether
// they still have to be given a way in.
type Invited struct {
	UserID uuid.UUID `json:"userId"`
	Email  string    `json:"email"`
	Status string    `json:"status"`
	At     time.Time `json:"at"`
	// Served is the address the call that invited this person was answered at,
	// port included, and "" when no call asked for the invitation. The subscriber
	// that mails the set-password link builds it from this: a link built from the
	// tenant's name alone opens port 80 on a development installation that serves
	// its tenants behind a port, and only the call knew the port it arrived on.
	// The call is the one that started the invitation, however far downstream this
	// event is published: an email-only sign-up creates the account in a worker, and
	// httpx.ServedFrom answers with the address the event that worker is handling
	// carried, so the port survives the handover. See auth's
	// contracts.ResetRequested.Served and httpx.ServedAuthority, which is where the
	// value is read off a request. Not a credential and not an address to trust:
	// the name in it is a name tenancy already resolved to this tenant, and the
	// only thing taken from it is the port.
	Served string `json:"served,omitempty"`
}

// PasswordSet is the payload of EventPasswordSet. It carries no password, no
// hash and no salt: what a subscriber may act on is that it happened and to
// whom, which is what an audit trail and a "your password changed" notice need.
type PasswordSet struct {
	UserID uuid.UUID `json:"userId"`
	At     time.Time `json:"at"`
}

// RolesSet is the payload of EventRolesSet: what this person may do has
// changed. Both sets are carried, because the interesting question about a
// grant is what it added.
type RolesSet struct {
	UserID uuid.UUID `json:"userId"`
	Was    []string  `json:"was"`
	Now    []string  `json:"now"`
	At     time.Time `json:"at"`
}

// HandleSet is the payload of EventHandleSet: the name this person answers to
// moved. Both names are carried, because the question a rename raises is who held
// it before — that is the whole reason the command exists as a command.
type HandleSet struct {
	UserID uuid.UUID `json:"userId"`
	Was    string    `json:"was"`
	Now    string    `json:"now"`
	At     time.Time `json:"at"`
}

// Deactivated is the payload of EventDeactivated: this person can no longer
// sign in.
type Deactivated struct {
	UserID uuid.UUID `json:"userId"`
	At     time.Time `json:"at"`
}

// AdministrationRefused is the payload of EventAdministrationRefused: somebody
// tried to take the last way a tenant has of administering itself away, and the
// write was refused.
//
// It names the person and the door, not the caller: actor, request id, client
// address and trace context ride on every outbox row already (kit/events), and
// modules/audit copies them into the trail, so repeating them here would be a
// second, drift-prone copy of facts the row already carries. The address is not
// here either — userId is the key every foreign key, subject and audit row
// already uses, and a trail reader can read the address from it. auth.login_failed
// carries an address only because a failed login has no user id to name.
//
// Roles are the person's roles that grant a tenant the ability to administer
// itself — the grant this write would have taken away. Nothing else is carried:
// the whole refusal is about those names, and the state that would have followed
// is one the refused write never created, so there is no "after" to report.
type AdministrationRefused struct {
	UserID uuid.UUID `json:"userId"`
	// Attempt is which door refused: "roles" (Service.SetRoles), "status"
	// (Service.Deactivate) or "delete" (the delete route's hook).
	Attempt string    `json:"attempt" enums:"roles,status,delete" example:"roles"`
	Roles   []string  `json:"roles"`
	At      time.Time `json:"at"`
}
