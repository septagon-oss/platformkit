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
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
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
//
// More than one composition can be live in a process — one application under two
// roles, or two applications on two databases — and what a publish is then checked
// against is the union of the compositions standing, not the list of whichever one
// booted last. The union is counted by grip rather than by name: the release of one
// boot gives back what that boot added and nothing another live composition is
// still answering under. That is the same scope pkit/claims.go gives the process's
// record of which application runs on which database, and for the same reason — the
// damage both refuse is one boot overwriting a fact another boot is using.
var (
	// catalogMu guards every change to the map below and to the counts beside it.
	// Readers take nothing.
	catalogMu sync.Mutex
	// catalog is the read side: a map nobody mutates once it is stored, replaced
	// whole by every change.
	catalog atomic.Pointer[map[string]*Schema]
	// declaredBy is how many live grips name each event. A name is in the catalog
	// exactly while its count is above zero, which is what makes one boot's release
	// stop at its own declarations.
	declaredBy = map[string]int{}
)

// DeclareAll installs the composition's declared events from nothing, so that every
// Publish can check a payload against its own event's schema: what is standing
// before the call is gone after it. kit/app's Start installs with DeclareMore
// instead, because a boot beside a live application has no right to take the shapes
// that application is answering under; this is the door for the state a process
// starts and ends with, and the one a test that publishes a declared name uses to
// install what it needs. An empty list leaves the check off, which is what a process
// with no modules composed has always done.
//
// It has no refusal to give, so a caller that hands it a composition's list hands it
// one already checked: CheckDeclared answers a list that disagrees with itself, and
// kit/app asks it before the first effect. Both doors join one name's spellings the
// same way, by declaredSchemas, because after that check every spelling of a name is
// one shape and the join says nothing about which entry stood.
func DeclareAll(list []Declared) {
	m, _ := declaredSchemas(list)
	catalogMu.Lock()
	defer catalogMu.Unlock()
	// The counts go with the map they counted: a grip held over a catalog that no
	// longer names its events has nothing left to give back, and release reads the
	// counts, never a snapshot.
	declaredBy = map[string]int{}
	catalog.Store(&m)
}

// CheckDeclared answers the refusal DeclareMore would give, over the declarations
// standing, and changes nothing. kit/app's New asks, so a composition that spells
// one of this process's event names another way is refused with its deployment still
// undialed rather than after the pool, the migration and the transport. A list that
// disagrees with itself is refused here too, by the same sentence: the promise is one
// shape per name, and nothing that reads the list gets to pick which spelling won.
func CheckDeclared(list []Declared) error {
	want, err := declaredSchemas(list)
	if err != nil {
		return err
	}
	catalogMu.Lock()
	defer catalogMu.Unlock()
	return clashes(want)
}

// DeclareMore installs a composition's declared events beside the ones this process
// already holds, as the last answer a boot gives before it opens anything (kit/app
// Start takes it above the connection and releases it on every refusal after it),
// and returns the release of what it added. It refuses, installing nothing, when the
// list spells an event this process already answers under with a different payload
// shape: one process holds one shape per event name, so the shapes a live
// application is answering publish under are neither replaced on the way in nor
// taken away on the way out, and a second application that means something else by
// a name the first one chose is the collision T-0231 removes by naming events for
// their application.
//
// The returned release is safe to call twice and from two goroutines; a boot that
// never installed anything returns nil, which is the release a caller may call
// anyway.
func DeclareMore(list []Declared) (func(), error) {
	want, err := declaredSchemas(list)
	if err != nil {
		return nil, err
	}
	catalogMu.Lock()
	defer catalogMu.Unlock()
	if err := clashes(want); err != nil {
		return nil, err
	}
	m := installed()
	names := make([]string, 0, len(want))
	for name, s := range want {
		m[name] = s
		declaredBy[name]++
		names = append(names, name)
	}
	catalog.Store(&m)
	var once sync.Once
	return func() {
		once.Do(func() {
			catalogMu.Lock()
			defer catalogMu.Unlock()
			left := installed()
			for _, name := range names {
				if declaredBy[name]--; declaredBy[name] > 0 {
					continue
				}
				delete(declaredBy, name)
				delete(left, name)
			}
			catalog.Store(&left)
		})
	}, nil
}

// declaredSchemas is one composition's list, name to projection, in the shape the
// catalog and the counts work in, and the refusal of a list that spells one name two
// ways. Taking the first spelling would be the catalog choosing which promise a
// composition keeps, which is the choice this package exists to make impossible; the
// disagreement comes back as an error instead, from the same OneShapePerName the
// manifest gate in kit/module reads, so one defect is refused in one sentence.
func declaredSchemas(list []Declared) (map[string]*Schema, error) {
	var errs []error
	for _, problem := range OneShapePerName(list) {
		errs = append(errs, errors.New("events: "+problem))
	}
	m := make(map[string]*Schema, len(list))
	for _, d := range list {
		if _, seen := m[d.Name]; !seen {
			m[d.Name] = d.Schema()
		}
	}
	return m, errors.Join(errs...)
}

// OneShapePerName names every event a list declares twice under payloads that are
// not one document, each with the shapes it was given, in the order the list named
// them: "integer then string" and "string then integer" are two different mistakes.
//
// It is the same promise clashes keeps between two compositions, made within one
// list, and it is the promise the manifest gate in kit/module reads: one event has
// one payload shape, so a manifest that writes one name twice has written two
// promises, and the composition that carries it is the thing that is wrong — not a
// list a caller could clean up by taking the first entry. Two spellings of one
// document are one event (sameShape compares the projection, not the Go type), and
// an event name declared with no payload type twice is one unchecked promise, so an
// honest manifest says the same thing twice and passes.
func OneShapePerName(list []Declared) []string {
	shapes := map[string][]*Schema{}
	var order []string
	for _, d := range list {
		if _, seen := shapes[d.Name]; !seen {
			order = append(order, d.Name)
		}
		shapes[d.Name] = append(shapes[d.Name], d.Schema())
	}
	var bad []string
	for _, name := range order {
		var distinct []*Schema
		for _, s := range shapes[name] {
			known := false
			for _, k := range distinct {
				if sameShape(k, s) {
					known = true
					break
				}
			}
			if !known {
				distinct = append(distinct, s)
			}
		}
		if len(distinct) < 2 {
			continue
		}
		texts := make([]string, 0, len(distinct))
		for _, s := range distinct {
			texts = append(texts, shapeText(s))
		}
		bad = append(bad, fmt.Sprintf("%s: declared %d times under %d payloads that are not one document (%s); one event has one payload shape, so the manifest that wrote it declares one shape or names two events",
			name, len(shapes[name]), len(distinct), strings.Join(texts, ", ")))
	}
	return bad
}

// installed is a copy of what stands, for the caller that is about to change it.
// Every stored map is read without a lock, so no change is ever made in place.
func installed() map[string]*Schema {
	out := map[string]*Schema{}
	if m := catalog.Load(); m != nil {
		maps.Copy(out, *m)
	}
	return out
}

// clashes refuses every name in want that this process already answers under a
// different shape, each with its own sentence, in name order so the same two
// compositions refuse the same way twice.
func clashes(want map[string]*Schema) error {
	standing := installed()
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(want)) {
		declared, ok := standing[name]
		if !ok || sameShape(declared, want[name]) {
			continue
		}
		errs = append(errs, fmt.Errorf("events: %s: this process answers it under %s and this composition declares %s; one process holds one shape per event name, so the application already standing is the one a second agrees with or is refused beside it (T-0231 names events for their application)",
			name, shapeText(declared), shapeText(want[name])))
	}
	return errors.Join(errs...)
}

// sameShape reports whether two declarations of one name describe one payload. The
// projection is compared, not the Go type: two modules with their own struct for
// the same document are one event, and refusing them would refuse the composition
// for a coincidence of naming. A declared type and no declared type are never one
// shape — a name one application leaves unchecked and another describes is exactly
// the disagreement the refusal is about.
func sameShape(a, b *Schema) bool {
	if a == nil || b == nil {
		return a == b
	}
	standing, err := json.Marshal(a)
	if err != nil {
		return false
	}
	arriving, err := json.Marshal(b)
	return err == nil && string(standing) == string(arriving)
}

// shapeText is a schema in the refusal's sentence: what the payload has to be, or
// the honest statement that nothing was declared.
func shapeText(s *Schema) string {
	if s == nil {
		return "no payload type"
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "a payload shape no document describes"
	}
	return string(b)
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
