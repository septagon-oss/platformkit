package contracts

import (
	"time"

	"github.com/google/uuid"
)

// The seven events this module emits. The first three are kit/rest's, published
// by the Spec module.go mounts; the last four are the lifecycle's. Both sets are
// listed in the manifest, and kit/app refuses to start if a route would publish
// one that is not.
const (
	EventCreated = "user.user.created"
	EventUpdated = "user.user.updated"
	EventDeleted = "user.user.deleted"

	EventInvited     = "user.invited"
	EventPasswordSet = "user.password_set"
	EventRolesSet    = "user.roles_set"
	EventDeactivated = "user.deactivated"
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

// Deactivated is the payload of EventDeactivated: this person can no longer
// sign in.
type Deactivated struct {
	UserID uuid.UUID `json:"userId"`
	At     time.Time `json:"at"`
}
