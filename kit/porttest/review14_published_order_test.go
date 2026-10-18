package porttest

import (
	"testing"

	"github.com/google/uuid"
)

// Review 14. `published` is the success case's whole assertion — the call worked and
// said exactly what the description says it says — and its comparison is
// `slices.Equal(said, want)`, which is a claim about the event *names and their
// order*. No test in this repository asks it. Measured at 5b08d62 by replacing that
// one comparison with `len(said) != len(want)`, the whole of kit/porttest and all
// three converted suites (fake world and real service alike) report ok: every
// operation in every suite here publishes exactly one event, so a length and a
// content agree on everything the repository can put in front of them. README's
// table says "the success case, in order", and the only thing standing between that
// sentence and its own deletion was this file.
//
// The two silences share a content comparison and are watched by silence_test.go;
// review 6 watched `published` against a port that said nothing about an event it
// published, and against one that named it twice. Neither reaches the order, and a
// length check passes both of those anyway.
//
// Both branches are here: the port that says what it described passes, so this
// cannot go green by failing everything, and the shortened list — the shape
// `published` guards with its other branch, "a published event is not withdrawn" —
// is asked in the same breath, since it is the one branch of the same function no
// other file reaches either.

// sayWorld is a port with nothing to it but what it said: no row, no grant, no
// revision, and an event list the case writes by hand, so the two shapes a length
// cannot tell from a comparison — the same events in another order, and a list that
// came back shorter than it was found — are both reachable.
type sayWorld struct{ said []string }

// saySuite describes one read that publishes: the order it owes is `want`, the
// order the port actually says them in is `utters`, and `found` is what the case
// starts with — an event an earlier command in the same world already said.
func saySuite(want []string, found []string, utters func(*sayWorld)) Suite[*sayWorld] {
	return Suite[*sayWorld]{
		Port:   "say.Service",
		World:  func(_ *testing.T, run func(*sayWorld)) { run(&sayWorld{}) },
		Events: func(w *sayWorld) []string { return append([]string(nil), w.said...) },
		Ops: []Op[*sayWorld]{{
			Name:      "Say",
			Publishes: want,
			Ready: func(_ *testing.T, w *sayWorld) uuid.UUID {
				w.said = append([]string(nil), found...)
				return uuid.Nil
			},
			Call: func(w *sayWorld, _ uuid.UUID) (string, error) {
				utters(w)
				return "said", nil
			},
		}},
	}
}

const saySuccess = "Say: the operation says what it did"

// TestSuccessCaseFailsWhenTheEventsAreTheSameOnesInAnotherOrder is the witness
// `published` had none of: the right events, the wrong order, and a comparison that
// is about both.
func TestSuccessCaseFailsWhenTheEventsAreTheSameOnesInAnotherOrder(t *testing.T) {
	// The honest branch first: the port says the two names in the order the
	// description gave them, and the case passes. Without this the test below
	// could be green because every case fails.
	honest := &transcript{failed: map[string][]string{}}
	run(watcher{t: t, log: honest}, saySuite(
		[]string{"note.filed", "note.sealed"}, nil,
		func(w *sayWorld) { w.said = []string{"note.filed", "note.sealed"} },
	))
	if said := honest.failures(saySuccess); len(said) > 0 {
		t.Fatalf("the order the description gave failed its own case: %v", said)
	}
	honest.mustPassApartFrom(t)

	log := &transcript{failed: map[string][]string{}}
	run(watcher{t: t, log: log}, saySuite(
		[]string{"note.filed", "note.sealed"}, nil,
		func(w *sayWorld) { w.said = []string{"note.sealed", "note.filed"} },
	))
	log.mustFail(t, saySuccess, "in this order")
	log.mustPassApartFrom(t, saySuccess)
}

// TestSuccessCaseFailsWhenTheCallTookAnEventAway asks the other branch of the same
// function — a list that came back shorter than the case found it, which is a
// published event withdrawn and a contradiction in words, not a slice index to walk
// past.
func TestSuccessCaseFailsWhenTheCallTookAnEventAway(t *testing.T) {
	already := []string{"note.filed"}
	honest := &transcript{failed: map[string][]string{}}
	run(watcher{t: t, log: honest}, saySuite([]string{"note.shelved"}, already,
		func(w *sayWorld) { w.said = append(w.said, "note.shelved") },
	))
	if said := honest.failures(saySuccess); len(said) > 0 {
		t.Fatalf("a read that published what it described failed its own case: %v", said)
	}
	honest.mustPassApartFrom(t)

	log := &transcript{failed: map[string][]string{}}
	run(watcher{t: t, log: log}, saySuite(nil, already,
		func(w *sayWorld) { w.said = nil },
	))
	log.mustFail(t, saySuccess, "a published event is not withdrawn")
	log.mustPassApartFrom(t, saySuccess)
}
