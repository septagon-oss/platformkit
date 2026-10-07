package contracts

import (
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events"
)

// The five events this module emits. The first three are kit/rest's, published
// by the Spec module.go mounts, and each carries the endpoint — identifiers, a
// name, an address and the event list, and never the secret, which is why the
// secret is not a field of the entity. The fourth is the fault rule's. The
// fifth is a person's act.
//
// There is no event per delivery attempt, and the absence is a decision: the
// log is the record of an attempt, a table the same administrator reads, and
// publishing an event for every POST would put a customer's slow web server into
// this platform's outbox at the customer's pace.
const (
	EventEndpointCreated  = "webhook.endpoint.created"
	EventEndpointUpdated  = "webhook.endpoint.updated"
	EventEndpointDeleted  = "webhook.endpoint.deleted"
	EventEndpointDisabled = "webhook.endpoint.disabled"
	EventEndpointEnabled  = "webhook.endpoint.enabled"
)

// Events is every event this module emits, for the manifest. kit/app refuses a
// route that would publish one not listed here.
var Events = []events.Declared{
	events.Declare[*Endpoint](EventEndpointCreated),
	events.Declare[*Endpoint](EventEndpointUpdated),
	events.Declare[*Endpoint](EventEndpointDeleted),
	events.Declare[EndpointDisabled](EventEndpointDisabled),
	events.Declare[*Endpoint](EventEndpointEnabled),
}

// EndpointDisabled is the payload of EventEndpointDisabled: an address stopped
// receiving, and the three facts that say why. It carries no secret and no
// delivery payload: the trail is a table an administrator reads for a year, and
// a customer's event bodies are not the platform's audit content.
type EndpointDisabled struct {
	EndpointID uuid.UUID `json:"endpointId"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	// Reason is the same sentence the endpoint row carries.
	Reason string `json:"reason"`
	// FailingSince is the first failure of the run that ended the endpoint, so
	// a reader can tell a three-day fault from a one-hour one.
	FailingSince time.Time `json:"failingSince"`
	At           time.Time `json:"at"`
}
