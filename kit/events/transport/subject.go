// subject.go is where an event's address is written down once. The envelope's
// CloudEvents `subject`, the broker's publish subject and a consumer's filter
// all come from these three functions, so a bridge that reads the address out of
// a document can subscribe to it and get the same messages.
package transport

import (
	"strings"

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

// NameOf returns the event name a subject carries, or "" when the subject is
// not a PlatformKit event address. It is Subject undone, and lives beside it for
// the same reason: two decoders of one address drift.
func NameOf(subject string) string {
	module, event, ok := strings.Cut(subject, ".")
	if !ok || module != SubjectPrefix {
		return ""
	}
	_, rest, _ := strings.Cut(event, ".") // the tenant segment
	if rest == "" || !ValidName(rest) {
		return ""
	}
	return rest
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
