package porttest

import "testing"

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

// Refused runs a step that must be refused, and asserts the four things a
// refusal owes: the error is the refusal named, it is in the class named, the
// snapshot did not move and nothing was published. That is house rule 9 as one
// assertion — a refused mutation writes nothing, emits nothing and returns no
// stale row — so no suite has to remember three of the four.
func (w World[W]) Refused(is func(error) bool, class Class, snapshot func() string, step func() error) {
	rep := w.report()
	rep.Helper()
	before, said := snapshot(), len(w.Events(w.Fixture))
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
	if now := w.Events(w.Fixture); len(now) != said {
		rep.Errorf("a refused call published %v; a refusal is not news", now[said:])
	}
}

// Silent runs a step that must change nothing at all, including the events. It
// is how a suite says "and this changed nothing", which is the claim a retry
// depends on.
func (w World[W]) Silent(what string, step func()) {
	rep := w.report()
	rep.Helper()
	before := len(w.Events(w.Fixture))
	step()
	if after := w.Events(w.Fixture); len(after) != before {
		rep.Errorf("%s published %v; repeating a command changes nothing, so it says nothing", what, after[before:])
	}
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
