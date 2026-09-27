package porttest

import (
	"slices"
	"testing"
)

// World is what the harness's assertions need of a world, so a module's own
// hand-written case gets the floor a generated case gets. Suite.Assert builds
// one; a case outside a suite may write the literal.
type World[W any] struct {
	// rep is how a generated case reports, so this package's own suite can watch
	// a case fail without failing the test that is watching. A World a module
	// wrote itself leaves it nil and reports straight at T.
	rep      reporter
	T        *testing.T
	Fixture  W
	Events   func(W) []string
	Classify func(error) Class
}

// Refused runs a step that must be refused, and asserts the three things a refusal
// owes that this harness has a channel for: the error is the refusal this case names
// (and, when the case names a class, that the error is in it), the snapshot did not
// move, and the event list is the one the call found, event for event (saidNothing). House rule 9's first two clauses as one
// assertion, so no suite has to remember them.
//
// The rule's third clause — a refusal returns no stale row — is not one of them, and
// claiming it here was the defect: Refusal.Call answers the error alone, so whatever
// a refused call handed back to its caller reaches no comparison in this package.
// "Writes nothing" is asserted of the store and of the events, and "returns no stale
// row" is asserted of neither. The three converted ports answer nil beside the error
// (sitetest.Save answers no row at all), so nothing shipped is unasserted; the channel
// would be Op.Call's rendering on the refusal side too, and review 6's pin assigns a
// func(W, uuid.UUID) error to that field, which the harness takes unchanged. So the
// clause is named here and in README, not asserted.
func (w World[W]) Refused(is func(error) bool, class Class, snapshot func() string, step func() error) {
	rep := w.report()
	rep.Helper()
	before, said := snapshot(), slices.Clone(w.Events(w.Fixture))
	err := step()
	switch {
	case err == nil:
		rep.Errorf("the call was not refused; it answered no error at all")
	case !is(err):
		rep.Errorf("the error is %v, which is not the refusal this case names", err)
	case class != Unclassified && w.Classify != nil:
		if got := w.Classify(err); got != class {
			rep.Errorf("%v is %s; this case names it %s, and a caller retypes one and not the other", err, got, class)
		}
	}
	if after := snapshot(); after != before {
		rep.Errorf("a refused call wrote: %s became %s", before, after)
	}
	w.saidNothing(rep, "a refused call", said, "a refusal is not news")
}

// saidNothing is house rule 9's second clause as one comparison, for the two cases
// that owe it — a refused call and the retry of a command that already ran — where
// it used to be spelled twice as a length check of their own. A length is not the
// claim: an outbox row rewritten rather than appended to leaves the list the same
// length and says something the caller never published, and a list that went down
// is a contradiction in words, not a slice index to walk past. The success case has
// always compared the events by content, in published; the two silences read the
// same list the same way now, so there is one place left to delete the clause from
// and one test file that watches it.
//
// was is the list as the step found it, and tail why this step owed its silence, so
// each failure says which rule it broke.
func (w World[W]) saidNothing(rep reporter, what string, was []string, tail string) {
	rep.Helper()
	now := w.Events(w.Fixture)
	switch {
	case len(now) > len(was):
		rep.Errorf("%s published %v; %s", what, now[len(was):], tail)
	case !slices.Equal(now, was):
		rep.Errorf("%s left %v where it found %v, and a step that says nothing rewrites no event: %s", what, now, was, tail)
	}
}

// Silent runs a step that must change nothing at all, including the events. It
// is how a suite says "and this changed nothing", which is the claim a retry
// depends on.
func (w World[W]) Silent(what string, step func()) {
	rep := w.report()
	rep.Helper()
	before := slices.Clone(w.Events(w.Fixture))
	step()
	w.saidNothing(rep, what, before, "repeating a command changes nothing, so it says nothing")
}

// Unchanged runs a step and asserts the snapshot either side of it is the same
// string.
func (w World[W]) Unchanged(what string, snapshot func() string, step func()) {
	rep := w.report()
	rep.Helper()
	before := snapshot()
	step()
	if after := snapshot(); after != before {
		rep.Errorf("%s wrote: %s became %s", what, before, after)
	}
}

func (w World[W]) report() reporter {
	if w.rep != nil {
		return w.rep
	}
	return live{w.T}
}

// reporter is everything the harness needs of testing.T: a place to report a
// failure, a place to run a subtest and the testing.T a module's own closure is
// handed. It exists so that this package's suite can assert that a generated
// case fails when the fake under it is broken — a harness whose own failures
// could not be observed would be a harness nobody could test.
//
// The harness never calls Fatalf. Every failure path here reports and returns,
// because a reporter that had to abort a goroutine would be a second mechanism
// for the same thing.
type reporter interface {
	T() *testing.T
	Helper()
	Errorf(format string, args ...any)
	Run(name string, f func(reporter)) bool
}

// live is the reporter every module's suite runs under: testing.T itself.
type live struct{ t *testing.T }

func (l live) T() *testing.T { return l.t }
func (l live) Helper()       { l.t.Helper() }

func (l live) Errorf(format string, args ...any) { l.t.Errorf(format, args...) }

func (l live) Run(name string, f func(reporter)) bool {
	return l.t.Run(name, func(t *testing.T) { f(live{t}) })
}
