// Attribution is what caused an event when the cause was not a signed-in person, and where
// the instruction came from.
//
// The actor column answers "whose session was this"; a seed run has none. Audit subscribes
// asynchronously to the durable event, so whatever its row will report has to be on that
// event: a source omitted at the moment of the write cannot be reconstructed later. It
// travels on the context rather than as an argument because the code that writes the event
// is the owner's own write path, several calls away from the loop that knows the source.
// See kit/tenancy.WithActor.
package events

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events/transport"
)

// Attribution is the carried form of a cause that was not a session; the
// transport holds the struct, the way it holds Event.
type Attribution = transport.Attribution

// The four kinds an attribution may name.
// migrations/000052_outbox_attribution.up.sql repeats them as a
// CHECK on the outbox column, and transport.Attribution.Valid is what refuses
// anything else before a statement is tried.
const (
	ActorUser   = transport.ActorUser
	ActorSystem = transport.ActorSystem
	ActorSeed   = transport.ActorSeed
	ActorJob    = transport.ActorJob
)

// MaxSourceFile is the ceiling transport.Attribution.Valid and the outbox
// column both hold a citation to.
const MaxSourceFile = transport.MaxSourceFile

// attributionKey is unexported: an Attribution is placed on a context by the
// code responsible for the write it describes, and nothing else reads it back to
// make a decision.
type attributionKey struct{}

// WithAttribution returns ctx carrying a. A zero ActorKind removes any
// attribution, so a handler running inside a request's context can say that the
// event it is about to write is its own and not the request's.
func WithAttribution(ctx context.Context, a Attribution) context.Context {
	if a.ActorKind == "" {
		return context.WithValue(ctx, attributionKey{}, nil)
	}
	return context.WithValue(ctx, attributionKey{}, a)
}

// AttributionFrom reads what WithAttribution put there.
func AttributionFrom(ctx context.Context) (Attribution, bool) {
	a, ok := ctx.Value(attributionKey{}).(Attribution)
	return a, ok
}

// nilUUID returns what a NULL column wants for an absent id: an untyped nil, so
// the driver binds NULL rather than the zero uuid. "Nobody" and "user
// 00000000-…" are different answers, and only the first is true.
func nilUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}
