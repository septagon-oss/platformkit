// subject.go is where an event's address is written down once. The envelope's
// CloudEvents `subject`, the broker's publish subject and a consumer's filter
// all come from the functions here, so a bridge that reads the address out of a
// document can subscribe to it and get the same messages. Nothing in this file
// reads an address back into an event name: nothing in the program needs to —
// the relay takes the name from the outbox row it is carrying, and a consumer's
// filter is derived from the name it subscribed with, never from a subject.
package transport

import (
	"github.com/google/uuid"
)

// SubjectPrefix is the one namespace every PlatformKit event is published
// under. A deployment that wants its own prefix on the broker gets it by
// renaming the stream, not by editing this: the envelope says which program
// produced it, and every consumer's filter is derived from here.
const SubjectPrefix = "platformkit"

// Subject is the address of one event:
//
//	platformkit.<tenant>.<module>.<event>
//
// The tenant segment is the point of the whole scheme (decision 0053 §1): a
// durable can be per tenant, one tenant's poison backlog cannot hold another's
// delivery behind it, and a bridge can route a customer's events by address
// rather than by opening the payload. The module and event halves are the
// event's Name, which is why Subject takes a name and not a second grammar.
func Subject(tenantID uuid.UUID, name string) string {
	return SubjectPrefix + "." + tenantID.String() + "." + name
}

// Filter is the wildcard a subscription to one event name uses: every tenant's
// delivery of that event, and no other event.
//
// It is a wildcard rather than one consumer per tenant because a consumer per
// tenant per event is a consumer count that grows with the customer base and
// drains nothing else: the outbox claim already makes one tenant's handler run
// once. What the segment buys is the *option* — an operator who needs a
// tenant's own backlog filters Subject(tenant, name), an exact subject rather
// than this wildcard, and the address space has held the tenant since this
// function landed.
func Filter(name string) string {
	return SubjectPrefix + ".*." + name
}

// Filters is every address a subscription to one event name has to answer, in
// the order a reader checks them: this build's, then the previous build's while
// a publisher on that build is still writing events this worker reads.
//
// The second is `platformkit.<module>.<event>` — the address before the subject
// carried a tenant segment. Nothing in this repository writes to it: the
// envelope never produces the old shape (see cloudevents.go); a publisher still
// running the previous build does. A NATS `*` matches exactly one token, so that
// message matches no consumer filtering `platformkit.*.<module>.<event>`, and
// there is no wildcard that means "this token, or none" and a stream subject
// transform cannot be added to a stream that already exists: a rolling window
// needs two filters and not a cleverer one.
//
// The window is the length of the rollout and no longer: the day no process
// running the previous build publishes, this list shrinks back to Filter alone
// and the consumer is remade without the second filter. Until then it is the
// only reason a pre-envelope message reaches a handler at all — the decoder
// that reads the old shape is unreachable without it, and an event the relay
// stamped published while nothing consumed it is the loss this whole change set
// exists to prevent, in a new place.
func Filters(name string) []string {
	return []string{Filter(name), SubjectPrefix + "." + name}
}
