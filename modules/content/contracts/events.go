package contracts

import (
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events/transport"
)

// The six events this module emits: kit/rest's three, published by the Spec
// module.go mounts, and the lifecycle's three, published by the commands. Both
// sets are in the manifest, and kit/app refuses to start if a route would
// publish one that is not.
const (
	EventCreated = "content.content.created"
	EventUpdated = "content.content.updated"
	EventDeleted = "content.content.deleted"

	EventPublished   = "content.published"
	EventUnpublished = "content.unpublished"
	EventArchived    = "content.archived"
)

// Events is every event this module emits, for the manifest.
var Events = []string{EventCreated, EventUpdated, EventDeleted, EventPublished, EventUnpublished, EventArchived}

// Payloads is the type of each of those events' payload, for the manifest and
// the documents the composition projects from it. kit/rest publishes the first
// three with the content itself; the three lifecycle events name Moved, which
// is one struct because the three differ in what happened and not in what a
// subscriber is told.
var Payloads = []transport.Declared{
	transport.Declare[Content](EventCreated),
	transport.Declare[Content](EventUpdated),
	transport.Declare[Content](EventDeleted),
	transport.Declare[Moved](EventPublished),
	transport.Declare[Moved](EventUnpublished),
	transport.Declare[Moved](EventArchived),
}

// Moved is the payload of all three lifecycle events: which content, what it
// is now, and when it moved. One struct rather than three, because the three
// events differ in what happened and not in what a subscriber needs to know —
// a cache invalidating a slug reads the same fields whichever it hears.
type Moved struct {
	ContentID uuid.UUID `json:"contentId"`
	Slug      string    `json:"slug"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	At        time.Time `json:"at"`
}
