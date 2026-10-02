// catalog.go is the promise a module makes about the shape of what it emits,
// and the check the outbox holds it to.
//
// A name alone is not a contract: modules/user could publish "user.invited" with
// any JSON in it and every subscriber would learn about it in production. So a
// manifest names each event with the Go type its payload is (Declared), the
// projection of that type is the schema (schema.go), and write — the one INSERT
// every event already passes through — refuses a payload that is not one. The
// catalog is not a second declaration anybody maintains: it is the manifest,
// read once at composition.
//
// The refusal is inside the caller's authoritative transaction, so a mis-shaped
// event rolls back the state change that would have caused it. That is the
// behaviour change worth naming: a publisher that gets this error has a bug in
// the code that built the payload, no retry fixes it, and the mutation it came
// with does not happen. Rule 9 in one sentence.
package events

import (
	"fmt"
	"reflect"
	"sync/atomic"
)

// Declared is one event a module emits: its name and the Go type of its
// payload. Declare is how a manifest writes one.
type Declared struct {
	Name string
	// Payload is the type Publish receives for this event. Nil is allowed and
	// means the module emits a payload the kernel cannot describe — a hand-built
	// JSON document, a type that marshals itself. The event is then published
	// unchecked, and it is listed as uncovered in the AsyncAPI document rather
	// than pretended to be.
	Payload reflect.Type
}

// Declare names an event and the Go type of its payload:
//
//	Declared: []events.Declared{events.Declare[contracts.Invited](contracts.EventInvited)}
//
// The type argument is the only way to say it that the compiler can check: a
// module that renames its payload type finds every one of its declarations
// refused at build time, which is the moment a contract should break.
func Declare[T any](name string) Declared {
	return Declared{Name: name, Payload: reflect.TypeFor[T]()}
}

// Names returns the event names of a list, for the callers that only need the
// set — a subscription check, an error message, a comparison against what the
// routes recorded.
func Names(list []Declared) []string {
	out := make([]string, len(list))
	for i, d := range list {
		out[i] = d.Name
	}
	return out
}

// Schema is the payload's projection, or nil when Payload describes nothing.
func (d Declared) Schema() *Schema { return SchemaOf(d.Payload) }

// catalog is the composition's declared events, resolved name to schema.
//
// It is a process fact, set by kit/app from the module list it validated once that
// list had started, and not a per-tenant or per-request one: what shape
// user.invited has is a property of the build, in the same way its route table is. A tenant, an
// actor, a locale or a feature flag never enters it, so there is nothing for a
// shared-instance run to unpick. The pointer swap makes install safe beside
// concurrent publishes; readers take no lock.
var catalog atomic.Pointer[map[string]*Schema]

// DeclareAll installs the composition's declared events, so that every Publish
// can check a payload against its own event's schema. kit/app's Start installs it
// with the union of every module manifest, as the last act of a boot that has
// nothing left to refuse: a composition the kernel refused then leaves the shapes
// a running application is already answering under exactly where they were, rather
// than replacing them on the way out. A test that publishes a declared name
// installs what it needs. An empty list leaves the check off, which is what a
// process with no modules composed has always done.
func DeclareAll(list []Declared) {
	m := make(map[string]*Schema, len(list))
	for _, d := range list {
		m[d.Name] = d.Schema()
	}
	catalog.Store(&m)
}

// checkPayload refuses a payload that is not what the event's own module
// declared. An event name no one declared is not refused here: the manifest
// gate in kit/app is what refuses a route that would publish it, and refusing
// twice here would only make the error harder to find.
func checkPayload(name string, body []byte) error {
	m := catalog.Load()
	if m == nil {
		return nil
	}
	s, ok := (*m)[name]
	if !ok || s == nil {
		return nil
	}
	if err := s.Validate(body); err != nil {
		return fmt.Errorf("events: %s: the payload is not what the module declared: %w", name, err)
	}
	return nil
}
