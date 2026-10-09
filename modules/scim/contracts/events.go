package contracts

import (
	"time"

	"github.com/google/uuid"
)

// The events this module emits, one per mutation of the state it owns: the
// collection, its membership, and this tenant's binding of a collection to a
// role. Everything about a *person* stays modules/user's event — a person a
// connector provisioned and a person an administrator invited are published by
// one name, and this module writes no user row of its own — so these four exist
// only for the facts no user event describes: a collection declared with nobody
// in it changes nobody's roles, and a withdrawn mapping is a decision about a
// role that no membership write says anything about.
//
// Each is written by `kit/events.Publish` inside the transaction that writes the
// row, so the state and its account commit together or not at all and a refused
// write emits nothing. The audit trail subscribes to every declared name, which
// is what makes a group write auditable at all: until now a group created at the
// protocol's door left no trace anywhere that anyone may be a member of it.
//
// The payloads carry identifiers, not content — see modules/audit's note on what
// a payload may hold, because that is the module that keeps these rows for a
// year. The group's name travels with its id because the row can be gone by the
// day somebody reads the trail, and the name is the only way a person recognises
// which collection it was.
const (
	EventGroupCreated   = "scim.group_created"
	EventGroupChanged   = "scim.group_changed"
	EventGroupRemoved   = "scim.group_removed"
	EventMappingChanged = "scim.mapping_changed"
)

// GroupCreated is the payload of EventGroupCreated: a collection the identity
// provider names now exists in this tenant, with however many members arrived
// beside it.
type GroupCreated struct {
	Group       uuid.UUID `json:"group"`
	DisplayName string    `json:"displayName"`
	ExternalID  string    `json:"externalId,omitempty"`
	Members     int       `json:"members"`
	At          time.Time `json:"at"`
}

// GroupChanged is the payload of EventGroupChanged: the membership arrived whole
// (a PUT, `Via` "replace") or one operation at a time (a PATCH, "patch"), which
// is the difference between a connector that says "this is the list" and one that
// says "add these". The two readings of the same final list mean different things
// to whoever reads the trail after a directory sync went wrong.
type GroupChanged struct {
	Group       uuid.UUID `json:"group"`
	DisplayName string    `json:"displayName"`
	Via         string    `json:"via"`
	Members     int       `json:"members"`
	At          time.Time `json:"at"`
}

// GroupRemoved is the payload of EventGroupRemoved. A group is refused while it
// has members, so the trail records the removal of a collection that had stopped
// covering anybody — and the mapping that went with it, if it had one.
type GroupRemoved struct {
	Group       uuid.UUID `json:"group"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role,omitempty"`
	At          time.Time `json:"at"`
}

// MappingChanged is the payload of EventMappingChanged: this tenant changed what
// a collection means. Both names are carried, because after the write the row
// says the new one and the trail is the only place left that knows what a
// membership used to grant — which is the question a stale role on a person asks
// the day somebody works out where it came from. Was "" is a binding made, Now
// "" is one withdrawn, and both halves are empty for a collection that is
// deleted with its mapping still in place.
type MappingChanged struct {
	Group       uuid.UUID `json:"group"`
	DisplayName string    `json:"displayName"`
	Was         string    `json:"was"`
	Now         string    `json:"now"`
	At          time.Time `json:"at"`
}
