package porttest

import (
	"slices"
	"testing"
)

// World.saidNothing is the one comparison behind "a refused command emits nothing"
// and "the retry says nothing". Every fake in this repository — this package's notes
// port included — publishes by appending a declared name to a Recorder, so the shape
// a suite can reach is "the call that was refused added one event": review 8's
// witness bites that one, through a real denial case of a real description. The two
// shapes below cannot be put on a Recorder by any knob: an event that was *rewritten*
// rather than appended to, and a list that came back *shorter*. Both are what house
// rule 9's second clause also forbids, and a length check — the comparison this file
// replaced — passes the first silently and panics on the second, slicing past the end
// of a list that shrank. Delete the content case from saidNothing and these three
// cases report green over an outbox row somebody overwrote.

// outbox is an event list a test can move between the two reads one assertion makes.
// (*outbox).Events is the Suite.Events shape over it, so the world under test is read
// the same way a module's fake reads its Recorder.
type outbox struct{ names []string }

func (o *outbox) Events() []string { return slices.Clone(o.names) }

// TestRefusedFailsWhenTheRefusalRewroteOrLostAnEvent witnesses the half of the
// refusal's silence no append can show. The honest refusal first, in each case, so
// the test cannot pass by failing everything: the same call that leaves the list
// alone passes, and the one that leaves a list of the same length but a different
// event — or fewer events, which used to be a slice panic — fails it.
func TestRefusedFailsWhenTheRefusalRewroteOrLostAnEvent(t *testing.T) {
	for _, tt := range []struct {
		what string
		left []string
	}{
		{"rewrote the event it had already published", []string{"note.shredded"}},
		{"lost the event it had already published", nil},
	} {
		t.Run(tt.what, func(t *testing.T) {
			honest := &transcript{failed: map[string][]string{}}
			quiet := &outbox{names: []string{"note.filed"}}
			World[*outbox]{rep: watcher{t: t, name: denialCase, log: honest}, Fixture: quiet, Events: (*outbox).Events}.
				Refused(func(error) bool { return true }, Unclassified, func() string { return "" }, func() error {
					return errDenied // refused, and the list is the one it found
				})
			if said := honest.failures(denialCase); len(said) > 0 {
				t.Fatalf("a refusal that left the events alone failed its own case: %v", said)
			}

			log := &transcript{failed: map[string][]string{}}
			box := &outbox{names: []string{"note.filed"}}
			World[*outbox]{rep: watcher{t: t, name: denialCase, log: log}, Fixture: box, Events: (*outbox).Events}.
				Refused(func(error) bool { return true }, Unclassified, func() string { return "" }, func() error {
					box.names = tt.left // refused, and it changed the list anyway
					return errDenied
				})
			log.mustFail(t, denialCase, "a refusal is not news")
			log.mustFail(t, denialCase, "rewrites no event")
		})
	}
}

// TestSilentFailsWhenTheStepRewroteAnEvent is the same comparison through the retry's
// silence, which shares it. TestRetryCaseFailsWhenTheSecondCallEmits already bites the
// append; this bites the rewrite, so neither half of the shared comparison is watched
// from only one of the two doors.
func TestSilentFailsWhenTheStepRewroteAnEvent(t *testing.T) {
	honest := &transcript{failed: map[string][]string{}}
	quiet := &outbox{names: []string{"note.filed"}}
	World[*outbox]{rep: watcher{t: t, name: retryCase, log: honest}, Fixture: quiet, Events: (*outbox).Events}.
		Silent("File a second time", func() {})
	if said := honest.failures(retryCase); len(said) > 0 {
		t.Fatalf("a step that changed nothing failed its own case: %v", said)
	}

	log := &transcript{failed: map[string][]string{}}
	box := &outbox{names: []string{"note.filed"}}
	World[*outbox]{rep: watcher{t: t, name: retryCase, log: log}, Fixture: box, Events: (*outbox).Events}.
		Silent("File a second time", func() { box.names = []string{"note.unfiled"} })
	log.mustFail(t, retryCase, "so it says nothing")
	log.mustFail(t, retryCase, "rewrites no event")
}

const (
	denialCase = "the denial case"
	retryCase  = "the retry case"
)
