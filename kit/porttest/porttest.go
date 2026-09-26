// Package porttest is the conformance harness a port's suite is written in: the
// description a module writes about its port, the cases this package generates
// from that description, and the assertions a module's own hand-written case
// reuses. The plumbing a module's fake embeds is in fake.go.
//
// It is not a capability module. It has no tenant, no entity, no migration and
// no module.go; its home is beside kit/db/dbtest, the kernel's other
// test-support package.
//
// # Why a description and not a generator
//
// Every question the harness asks of a port is asked through a closure the
// module wrote, in Go, beside the port it is about: no string this package
// parses, no struct tag, no expression tree and no generated file. What that
// buys is the four refusals every mutating operation owes — an unknown row, a
// caller with no grant, a revision that moved, another tenant's row — asserted
// structurally instead of whenever a suite's author remembered them.
//
// What it deliberately cannot express is a sequence: a description rich enough
// to generate "gather the service, its figures, their reviews, ask the register,
// then write the supersession and two events" would be a second language, less
// legible than the decision function a reader can read today. A case the
// description cannot express goes in Suite.Own with the reason it is written by
// hand, and the harness refuses a suite that leaves the reason out.
package porttest

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// Suite is one port's description: the world a case runs in, the operations the
// port declares, and the cases a description cannot express.
//
// W is the module's own fixture type. The harness never looks inside it.
type Suite[W any] struct {
	// Port is the name a generated case prints, e.g. "contracts.Service".
	Port string

	// World builds one case's world, calls run with it and closes what it
	// opened. The harness calls it once per case, so no case sees another's
	// rows. The real service's world is a transaction and a transaction is a
	// scope somebody has to close, which is why the world calls the case rather
	// than returning to it.
	World func(t *testing.T, run func(W))

	// Events is what the implementation has published so far, in order: the fake
	// returns what it recorded, the SQL world reads the outbox rows its
	// transaction wrote.
	Events func(W) []string

	// Classify is the module's own reading of one of its refusals. A port whose
	// refusals carry no classification returns Unclassified for every error and
	// the generated cases assert Refusal.Is alone.
	Classify func(error) Class

	// Invariant is the assertion the module makes of any state at all, run after
	// every case — generated or its own — against the row the case acted on. Nil
	// where a port has no such statement.
	Invariant func(t *testing.T, w W, row uuid.UUID)

	// Ops is the port's operations, in the order a reader of the port meets them.
	Ops []Op[W]

	// Own is the cases the description cannot express, each with the reason it
	// stays hand-written. They run through the same World, under the name the
	// module gives, neither renamed nor nested: a case name is a requirement's
	// evidence in a client repository.
	Own []Case[W]
}

// Case is one hand-written case and the reason the description cannot express it.
type Case[W any] struct {
	// Name is the subtest name, used verbatim.
	Name string
	// Because is why this case is not generated. The harness fails a suite whose
	// own case gives no reason: "the harness cannot express this" is a sentence
	// somebody writes down, not an empty field.
	Because string
	Run     func(t *testing.T, w W)
}

// Op is one operation of the port. Every field is read by a generated case named
// in this package's README table; a field no case reads does not belong here.
type Op[W any] struct {
	// Name is the method's own name, unique within a Suite, and the first half of
	// every generated case name the module does not name itself.
	Name string

	// Mutates says whether the operation writes. A read is not asked to be
	// idempotent, is not asked for a revision and is not asked to publish.
	Mutates bool

	// Ready puts the world into the state the operation succeeds from and returns
	// the row it acts on. Every generated case of this operation starts here.
	Ready func(t *testing.T, w W) uuid.UUID

	// Call runs the operation against the row Ready returned, as an actor who
	// holds what it needs. The harness calls it once for the success case and
	// twice for the retry case.
	Call func(w W, row uuid.UUID) error

	// Snapshot is everything this operation could have written, rendered so that
	// two snapshots of one world compare with ==. It is the module's answer to
	// "a refused mutation writes nothing": the harness reads it either side of
	// every refused call and either side of a retry. A read may leave it nil, and
	// then its refusals assert silence alone.
	Snapshot func(t *testing.T, w W, row uuid.UUID) string

	// Publishes is the event names one successful Call must publish, in order. An
	// empty list says the operation publishes nothing, and the harness asserts
	// that too.
	Publishes []string

	// Refusals are this operation's refusals: the four the floor requires, by
	// Kind, and however many of the module's own it names.
	Refusals []Refusal[W]

	// Names is the module's own sentence for a generated case, by Kind. Empty
	// means the harness names the case itself. A sentence the module gives is
	// used verbatim, because in a client repository a case name is what a
	// requirements index points at: a module that already says "the same
	// placement twice says nothing twice" keeps saying it, and the evidence link
	// keeps pointing at something.
	Names map[Kind]string

	// Skip records, by case shape or refusal Kind, why this operation is not
	// asked that case. The harness fails a suite that leaves a required case out
	// with no reason, and fails a reason with no case left out.
	Skip map[Kind]string
}

// Refusal is one refusal an operation owes: the call that provokes it, the
// module's own test of whether the error is that refusal, and its class.
type Refusal[W any] struct {
	// Kind is the floor this refusal answers, or Named for one of the module's
	// own.
	Kind Kind

	// Name is the case name, used verbatim. Required for a Named refusal;
	// optional for a floor Kind, which the harness can name itself.
	Name string

	// Provoke moves the world into the state this refusal answers, if it is not
	// already there, and returns the row the refused call names. It asserts
	// nothing: a step that asserts something on the way is a case of its own.
	Provoke func(t *testing.T, w W, row uuid.UUID) uuid.UUID

	// Call is the refused call.
	Call func(w W, row uuid.UUID) error

	// Is reports whether the error the call returned is this refusal. A port that
	// names sentinels writes errors.Is; a port whose refusals carry a field and a
	// classification compares those. The harness never unwraps an error itself:
	// what counts as the same refusal is the port's own answer, and a harness
	// that guessed would relax an assertion.
	//
	// It takes the world as well as the error because some refusals quote a fact
	// only the world knows — "this content is already publication 3" — and a
	// predicate that could not read the world would have to drop that half of
	// the assertion.
	Is func(w W, err error) bool

	// Class is correctable or immutable, asserted through Suite.Classify.
	Class Class
}

// Kind names one case the harness generates, and is the key a Skip reason is
// filed under.
type Kind string

// The floor: the four refusals every mutating operation owes, and the two shapes
// every operation owes. Nothing here is domain-specific, which is why the kernel
// can generate them.
const (
	Unknown   Kind = "an unknown row is not found"
	Denied    Kind = "a caller with no grant is refused and writes nothing"
	Stale     Kind = "a revision the row is not at is refused and writes nothing"
	Elsewhere Kind = "another tenant cannot reach the row"
	Success   Kind = "the operation says what it did"
	Retry     Kind = "the same command twice writes nothing and says nothing"
	Named     Kind = "" // one of the module's own refusals
)

// floor is the four refusals a mutating operation owes, in the order they are
// generated.
var floor = []Kind{Unknown, Denied, Stale, Elsewhere}

// Class is what a module says about a refusal: whether the caller can repair the
// request (Correctable) or the state forbids it however the form is retyped
// (Immutable).
type Class int

const (
	Unclassified Class = iota
	Correctable
	Immutable
)

func (c Class) String() string {
	switch c {
	case Correctable:
		return "correctable"
	case Immutable:
		return "immutable"
	default:
		return "unclassified"
	}
}

// Run is the harness. It fails an incomplete description before it runs a case,
// then runs the cases the description generates followed by the module's own.
func Run[W any](t *testing.T, s Suite[W]) {
	t.Helper()
	run(live{t}, s)
}

// Names is every case name Run would run, in order, without running one. A
// module pins the list in a test of its own so that a change to a case name —
// which is a requirement's evidence — is a diff somebody reads.
func Names[W any](s Suite[W]) []string {
	plan := s.plan()
	names := make([]string, 0, len(plan))
	for _, c := range plan {
		names = append(names, c.name)
	}
	return names
}

// Assert is the floor a generated case gets, handed to a case the description
// cannot express: a module's own case builds one from the suite it belongs to
// rather than restating Events and Classify beside it.
func (s Suite[W]) Assert(t *testing.T, w W) World[W] {
	return World[W]{T: t, Fixture: w, Events: s.Events, Classify: s.Classify}
}

// world is Assert for a generated case, which reports through the harness's own
// reporter rather than straight at testing.T, so this package's suite can watch
// a case fail.
func (s Suite[W]) world(rep reporter, w W) World[W] {
	return World[W]{rep: rep, T: rep.T(), Fixture: w, Events: s.Events, Classify: s.Classify}
}

// run is Run against any reporter, so this package's own suite can watch a
// generated case fail without failing the test that is watching it.
func run[W any](rep reporter, s Suite[W]) {
	rep.Helper()
	if problems := s.problems(); len(problems) > 0 {
		for _, p := range problems {
			rep.Errorf("%s", p)
		}
		return
	}
	for _, c := range s.plan() {
		body := c.body
		rep.Run(c.name, func(rep reporter) {
			s.World(rep.T(), func(w W) {
				if reflect.ValueOf(&w).Elem().IsZero() {
					rep.Errorf("%s: the world handed back a zero fixture, so every case below it would assert nothing", s.Port)
					return
				}
				body(rep, w)
			})
		})
	}
}

// planned is one case the harness will run: the name it runs under, and the body
// the world calls. Names and Run read the same list, so a pinned name list is
// the list that runs.
type planned[W any] struct {
	name string
	body func(rep reporter, w W)
}

// plan is every case, in the order §4 of the specification names: per operation
// the success, the retry, the four floor refusals it describes and its own named
// refusals; then the module's hand-written cases.
func (s Suite[W]) plan() []planned[W] {
	var plan []planned[W]
	for _, op := range s.Ops {
		plan = append(plan, planned[W]{s.caseName(op, Success), func(rep reporter, w W) { s.success(rep, op, w) }})
		if op.Mutates && op.Skip[Retry] == "" {
			plan = append(plan, planned[W]{s.caseName(op, Retry), func(rep reporter, w W) { s.retry(rep, op, w) }})
		}
		for _, kind := range floor {
			if r, ok := op.refusal(kind); ok {
				plan = append(plan, planned[W]{s.refusalName(op, r), func(rep reporter, w W) { s.refused(rep, op, r, w) }})
			}
		}
		for _, r := range op.Refusals {
			if r.Kind != Named {
				continue
			}
			plan = append(plan, planned[W]{s.refusalName(op, r), func(rep reporter, w W) { s.refused(rep, op, r, w) }})
		}
	}
	for _, c := range s.Own {
		plan = append(plan, planned[W]{c.Name, func(rep reporter, w W) {
			c.Run(rep.T(), w)
			s.invariant(rep.T(), w, uuid.Nil)
		}})
	}
	return plan
}

// caseName is the module's own sentence for a generated case where it has one,
// and the harness's sentence under the operation's name where it has not.
func (s Suite[W]) caseName(op Op[W], kind Kind) string {
	if own := op.Names[kind]; own != "" {
		return own
	}
	return op.Name + ": " + string(kind)
}

func (s Suite[W]) refusalName(op Op[W], r Refusal[W]) string {
	if r.Name != "" {
		return r.Name
	}
	return op.Name + ": " + string(r.Kind)
}

func (op Op[W]) refusal(kind Kind) (Refusal[W], bool) {
	for _, r := range op.Refusals {
		if r.Kind == kind {
			return r, true
		}
	}
	return Refusal[W]{}, false
}

// success is case 1: the call the port exists for works, and says exactly what
// the description says it says.
func (s Suite[W]) success(rep reporter, op Op[W], w W) {
	rep.Helper()
	row := op.Ready(rep.T(), w)
	before := len(s.Events(w))
	if err := op.Call(w, row); err != nil {
		rep.Errorf("%s: %v; this is the call the operation is for", op.Name, err)
		return
	}
	s.published(rep, op.Name, w, before, op.Publishes)
	s.invariant(rep.T(), w, row)
}

// retry is case 2: the same command twice. The second call succeeds, writes
// nothing and — the half a return value cannot show — says nothing, because an
// idempotent command that publishes is a subscriber told twice about one thing.
func (s Suite[W]) retry(rep reporter, op Op[W], w W) {
	rep.Helper()
	row := op.Ready(rep.T(), w)
	if err := op.Call(w, row); err != nil {
		rep.Errorf("%s: the first call: %v", op.Name, err)
		return
	}
	world := s.world(rep, w)
	world.Silent(op.Name+" a second time", func() {
		world.Unchanged(op.Name+" a second time", s.snapshot(rep.T(), op, w, row), func() {
			if err := op.Call(w, row); err != nil {
				rep.Errorf("%s: the same command twice: %v; a command that refuses its own retry cannot be retried", op.Name, err)
			}
		})
	})
	s.invariant(rep.T(), w, row)
}

// refused is cases 3 to 7: a call that must be refused, asserted through the
// same World a module's own case uses, so "refused" means one thing here.
func (s Suite[W]) refused(rep reporter, op Op[W], r Refusal[W], w W) {
	rep.Helper()
	row := op.Ready(rep.T(), w)
	if r.Provoke != nil {
		row = r.Provoke(rep.T(), w, row)
	}
	name := s.refusalName(op, r)
	world := s.world(rep, w)
	snapshot := s.snapshot(rep.T(), op, w, row)
	world.Refused(func(err error) bool { return r.Is(w, err) }, r.Class, snapshot, func() error {
		return r.Call(w, row)
	})
	// The tenant case owes one assertion more than the other three: a refusal
	// that took the row away with it would leave an unchanged snapshot of
	// nothing, and the row has to still be there for the tenant that owns it.
	if r.Kind == Elsewhere && op.Snapshot != nil && snapshot() == "" {
		rep.Errorf("%s: after another tenant's refused call the row renders as nothing; it is still this tenant's row", name)
	}
	s.invariant(rep.T(), w, row)
}

// snapshot is the operation's own rendering of everything it could have written,
// or a constant for a read that declares none.
func (s Suite[W]) snapshot(t *testing.T, op Op[W], w W, row uuid.UUID) func() string {
	if op.Snapshot == nil {
		return func() string { return "" }
	}
	return func() string { return op.Snapshot(t, w, row) }
}

// published asserts the events one call said are exactly the ones described, in
// order.
func (s Suite[W]) published(rep reporter, what string, w W, before int, want []string) {
	rep.Helper()
	all := s.Events(w)
	if len(all) < before {
		rep.Errorf("%s: the events went from %d to %d; a published event is not withdrawn", what, before, len(all))
		return
	}
	if said := all[before:]; !slices.Equal(said, want) {
		rep.Errorf("%s published %v, want %v in this order", what, said, want)
	}
}

func (s Suite[W]) invariant(t *testing.T, w W, row uuid.UUID) {
	if s.Invariant != nil {
		s.Invariant(t, w, row)
	}
}

// problems is every reason this description cannot be run, all of them at once
// so one run fixes one description. It is house rule 8 made mechanical: the
// write that takes the last assertion away is refused, and a port that genuinely
// has no such case writes the reason down and is accepted.
func (s Suite[W]) problems() []string {
	var out []string
	say := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	if s.Port == "" {
		say("porttest: the suite names no port, and every case it fails would name nothing")
	}
	if s.World == nil {
		say("%s: the suite builds no world", s.Port)
	}
	if s.Events == nil {
		say("%s: the suite cannot read what the port published, so half of every case is unassertable", s.Port)
	}
	seen := map[string]string{}
	for _, op := range s.Ops {
		if op.Name == "" {
			say("%s: an operation with no name", s.Port)
			continue
		}
		if was, dup := seen[op.Name]; dup {
			say("%s: two operations named %q (%s)", s.Port, op.Name, was)
		}
		seen[op.Name] = "operation"
		out = append(out, op.problems(s.Port)...)
	}
	for _, c := range s.Own {
		switch {
		case c.Name == "":
			say("%s: a case of the module's own with no name", s.Port)
		case c.Because == "":
			say("%s: %q is hand-written and says no reason; write why the description cannot express it", s.Port, c.Name)
		case c.Run == nil:
			say("%s: %q has no body", s.Port, c.Name)
		}
	}
	for _, c := range s.plan() {
		if was, dup := seen[c.name]; dup && was == "case" {
			say("%s: two cases would run under the name %q, and a requirements index cannot point at either", s.Port, c.name)
		}
		seen[c.name] = "case"
	}
	return out
}

// problems is one operation's share: the floor it owes, the fields a case reads,
// and the skips it claims.
func (op Op[W]) problems(port string) []string {
	var out []string
	say := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	if op.Ready == nil {
		say("%s %s: no Ready, so no case knows the state the operation succeeds from", port, op.Name)
	}
	if op.Call == nil {
		say("%s %s: no Call", port, op.Name)
	}
	if op.Mutates && op.Snapshot == nil {
		say("%s %s: a mutating operation with no Snapshot; \"a refused mutation writes nothing\" is unassertable without one", port, op.Name)
	}
	if reason, skipped := op.Skip[Success]; skipped {
		say("%s %s: the success is not optional, and %q does not excuse it", port, op.Name, reason)
	}
	if _, skipped := op.Skip[Named]; skipped {
		say("%s %s: a skip filed under Named names no case; a refusal of the module's own is described or it is not owed", port, op.Name)
	}
	names := map[string]bool{}
	for _, r := range op.Refusals {
		if r.Kind == Named && r.Name == "" {
			say("%s %s: a refusal of the module's own with no name", port, op.Name)
		}
		if r.Name != "" && names[r.Name] {
			say("%s %s: two refusals named %q", port, op.Name, r.Name)
		}
		names[r.Name] = true
		if r.Call == nil {
			say("%s %s: the refusal %q makes no call", port, op.Name, r.Name)
		}
		if r.Is == nil {
			say("%s %s: the refusal %q has no Is, and what counts as that refusal is the port's own answer", port, op.Name, r.Name)
		}
		if reason, skipped := op.Skip[r.Kind]; skipped && r.Kind != Named {
			say("%s %s: %q is described and skipped (%q); one of the two is wrong", port, op.Name, string(r.Kind), reason)
		}
	}
	for kind, reason := range op.Skip {
		if reason == "" {
			say("%s %s: a skip of %q with no reason; the reason is the case", port, op.Name, string(kind))
		}
	}
	if !op.Mutates {
		return out
	}
	for _, kind := range floor {
		if _, ok := op.refusal(kind); ok {
			continue
		}
		if op.Skip[kind] == "" {
			say("%s %s: a mutating operation owes %q and neither describes it nor says why not", port, op.Name, string(kind))
		}
	}
	return out
}
