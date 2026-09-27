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
	// the generated cases assert Refusal.Is alone. A suite whose refusals do name
	// a class has to supply it: Run refuses one that does not, because the class
	// would then be asserted by nobody and skipped by the case in silence.
	Classify func(error) Class

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
	// holds what it needs, and renders what the operation answered so that two
	// answers compare with ==. The harness calls it once for the success case
	// and twice for the retry, where the second answer must equal the first: a
	// command that stores the right row and answers a moved one returns a stale
	// row to its caller, which is house rule 9's third clause and the half no
	// snapshot of the store can see. A mutating operation that renders the empty
	// string fails its own retry, because two empty strings compare equal and the
	// case would assert the store alone: a command that answers no row still
	// renders a word for what it answered.
	Call func(w W, row uuid.UUID) (answer string, err error)

	// Snapshot is everything this operation could have written, rendered so that
	// two snapshots of one world compare with ==. It is the module's answer to
	// "a refused mutation writes nothing": the harness reads it either side of
	// every refused call and either side of a retry. A read may leave it nil, and
	// then its refusals assert silence alone. Every case that reads it asks it
	// first whether it can tell the row it acts on from a row nobody seeded, since
	// a rendering that cannot would make the comparison one nothing can move.
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

	// Call is the refused call. It answers the error alone: whatever the call handed
	// back beside that error is discarded here and compared by nothing, which is why
	// World.Refused asserts what a refusal left in the store and in the events and not
	// what it returned — see that function and "What the floor does not assert" in the
	// README.
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
			ran := false
			s.World(rep.T(), func(w W) {
				ran = true
				if reflect.ValueOf(&w).Elem().IsZero() {
					rep.Errorf("%s: the world handed back a zero fixture, so every case below it would assert nothing", s.Port)
					return
				}
				body(rep, w)
			})
			// A world is a transaction-shaped closure with error paths in it,
			// and one that returns without calling the case would report it
			// green having touched no implementation at all.
			if !ran {
				rep.Errorf("%s: the world returned without running this case, so it passed against nothing", s.Port)
			}
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
	// snapshotSeesTheRow exempts a row Ready does not name from the row witness, because a
	// per-tenant singleton's Snapshot has no row argument to read. The exemption costs such a
	// port its row argument and not its assertion, so the one question a rendering with no row
	// argument can still answer is asked here, through the same closure every "writes nothing"
	// comparison reads: did it move across a call this case records as a success. One that did
	// not move renders it the same either side of every refusal, and every one of those
	// comparisons is then one nothing can move — which is how a singleton port loses its whole
	// assertion without anybody deleting one.
	rowless := op.Mutates && row == uuid.Nil
	var was string
	if rowless {
		was = s.snapshot(rep.T(), op, w, row)()
	}
	if _, err := op.Call(w, row); err != nil {
		rep.Errorf("%s: %v; this is the call the operation is for", op.Name, err)
		return
	}
	if rowless && s.snapshot(rep.T(), op, w, row)() == was {
		rep.Errorf("%s: the Snapshot renders %q both before and after a call this case records as a "+
			"success, so \"writes nothing\" compares two renderings nothing can move; render what the "+
			"operation writes", op.Name, was)
	}
	s.published(rep, op.Name, w, before, op.Publishes)
}

// retry is case 2: the same command twice. The second call succeeds, writes
// nothing, says nothing — an idempotent command that publishes is a subscriber
// told twice about one thing — and answers what the first call answered, which
// is the half neither a snapshot of the store nor an error can show. It refuses
// an answer that renders nothing first, because an assertion made between two
// empty strings is not an assertion and a port can lose this one by emptying a
// rendering rather than by deleting a comparison.
func (s Suite[W]) retry(rep reporter, op Op[W], w W) {
	rep.Helper()
	row := op.Ready(rep.T(), w)
	s.snapshotSeesTheRow(rep, op, w, row)
	first, err := op.Call(w, row)
	if err != nil {
		rep.Errorf("%s: the first call: %v", op.Name, err)
		return
	}
	if first == "" {
		rep.Errorf("%s: the first call rendered the empty string as its answer. The case compares what the "+
			"two calls answered, and two empty strings always compare equal, so it would assert the store "+
			"and nothing about the answer; render what the command answered, and a command that answers no "+
			"row renders a word for that", op.Name)
	}
	world := s.world(rep, w)
	world.Silent(op.Name+" a second time", func() {
		world.Unchanged(op.Name+" a second time", s.snapshot(rep.T(), op, w, row), func() {
			again, err := op.Call(w, row)
			if err != nil {
				rep.Errorf("%s: the same command twice: %v; a command that refuses its own retry cannot be retried", op.Name, err)
				return
			}
			if again != first {
				rep.Errorf("%s: the first call answered %s and the second %s; a retry answers the row as it stands, "+
					"and a caller told otherwise was handed a row nothing wrote", op.Name, first, again)
			}
		})
	})
}

// refused is cases 3 to 7: a call that must be refused, asserted through the
// same World a module's own case uses, so "refused" means one thing here.
func (s Suite[W]) refused(rep reporter, op Op[W], r Refusal[W], w W) {
	rep.Helper()
	seeded := op.Ready(rep.T(), w)
	s.snapshotSeesTheRow(rep, op, w, seeded)
	// The tenant case watches the row from the moment it is seeded — before
	// Provoke moves the world, which is a step that may run the implementation —
	// until the refused call has answered.
	var watched []func() string
	if r.Kind == Elsewhere {
		watched = append(watched, storeWitness.watch(seeded))
	}
	row := seeded
	if r.Provoke != nil {
		row = r.Provoke(rep.T(), w, row)
		if r.Kind == Elsewhere && row != seeded {
			watched = append(watched, storeWitness.watch(row))
		}
	}
	name := s.refusalName(op, r)
	// The tenant case owes one assertion more than the other three: the row has
	// to still be there for the tenant that owns it. The operation's Snapshot
	// cannot make it, because Provoke has just moved the world into another
	// tenant's and the snapshot below reads through that world — either side of
	// the refused call it renders the row absent, whichever way the refusal went,
	// so "wrote nothing" here is written from the visitor's side of the refusal.
	// The store is the one party that cannot be moved by a description, so the
	// case asks it whether the row is still there.
	world := s.world(rep, w)
	snapshot := s.snapshot(rep.T(), op, w, row)
	world.Refused(func(err error) bool { return r.Is(w, err) }, r.Class, snapshot, func() error {
		return r.Call(w, row)
	})
	// Reported after the shared floor, so a case that was refused badly says that
	// first and only then says what the refusal did to the row.
	for _, stop := range watched {
		if lost := stop(); lost != "" {
			rep.Errorf("%s: another tenant's refused call took the row away: %s. A refusal writes nothing, and the row has to still be there for the tenant that owns it", name, lost)
		}
	}
}

// snapshotSeesTheRow is the harness asking the description's Snapshot whether it
// can tell the row this case seeded from a row nobody ever seeded. Every "writes
// nothing" assertion in the retry and in every refusal compares two renderings of
// that one function, so a Snapshot that renders the same string for both makes
// them assertions about nothing: a fake can lose the row, the whole port can lose
// the row, and the suite stays green behind it. This is the hole 32aee9b closed
// for an answer that rendered nothing — the empty Call made the retry compare two
// empty strings — in the field that actually carries the assertion, and it is said
// on every case whose assertion rests on the rendering.
//
// A row Ready does not name has no identity for a Snapshot to look at — a
// per-tenant singleton's snapshot has no row argument to read — and is not asked. What that
// exemption leaves unasked is asked in the success case, which reads this same closure either
// side of a call the description records as a write: see success.
// The check is sensitive to the row argument by design: a Snapshot that renders
// the whole world rather than the row would be refused here too, and the
// correction it is told is to render the row, because a rendering that cannot
// name one row cannot show that a refused call left it alone.
func (s Suite[W]) snapshotSeesTheRow(rep reporter, op Op[W], w W, row uuid.UUID) {
	rep.Helper()
	if op.Snapshot == nil || row == uuid.Nil {
		return
	}
	seen := op.Snapshot(rep.T(), w, row)
	if anon := op.Snapshot(rep.T(), w, uuid.New()); anon == seen {
		rep.Errorf("%s: the Snapshot renders %q both for the row this case seeded and for a row nobody seeded, so "+
			"\"writes nothing\" compares two renderings nothing can move; render the row the case names", op.Name, seen)
	}
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
		out = append(out, op.problems(s.Port, s.Classify != nil)...)
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
	plan := s.plan()
	if len(plan) == 0 {
		say("%s: the description runs no case at all; a suite that asserts nothing passes every implementation "+
			"of the port, including the one that does nothing", s.Port)
	}
	for _, c := range plan {
		if was, dup := seen[c.name]; dup && was == "case" {
			say("%s: two cases would run under the name %q, and a requirements index cannot point at either", s.Port, c.name)
		}
		seen[c.name] = "case"
	}
	return out
}

// problems is one operation's share: the floor it owes, the fields a case reads,
// and the skips it claims. classifies says whether the suite can answer what
// class one of these refusals is in, which is the half of a named class a case
// cannot assert on its own.
func (op Op[W]) problems(port string, classifies bool) []string {
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
		if r.Class != Unclassified && !classifies {
			say("%s %s: the refusal %q names the class %s and the suite classifies nothing, so no case would ever ask "+
				"whether the error is in it; give the suite a Classify or leave the class out", port, op.Name, r.Name, r.Class)
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
