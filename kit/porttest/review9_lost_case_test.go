package porttest

import (
	"slices"
	"strings"
	"testing"
)

// Review 9. The one promise this package makes above generating cases is that no
// case is lost — `problems` refuses every way a description can quietly stop
// asserting (a missing floor case with no reason, a reason with no case, two
// cases under one name, a suite that runs nothing). Two ways round it are open,
// and both were checked by running a description rather than by reading it:
//
//  1. `plan` walks the floor Kinds and takes the FIRST refusal of each
//     (`op.refusal` returns the first match), then plans only the refusals whose
//     Kind is `Named`. A description that makes two refusals of one floor Kind —
//     the same refusal owed from two states, which is how tasktest ended up
//     writing "a resolved task cannot be assigned" and "a closed task cannot be
//     assigned" as two entries rather than a loop — runs one and never runs the
//     other. `problems` does not see it: it refuses two refusals sharing a *name*,
//     and a floor refusal carries no name, so the entries differ only in a
//     sentence nobody compares. The lost case is absent from `Names` too, so the
//     module's own pin pins the shortened list and calls it the suite.
//
//  2. `caseName` reads `Op.Names` for `Success` and `Retry` only; a floor refusal
//     is named by `Refusal.Name`. A description that puts its sentence for
//     `Unknown` in `Names`, where the map's key type and this package's README
//     table both invite it, has that sentence dropped silently: the case runs
//     under the harness's own words, and a requirements index in a client
//     repository points at the sentence the module wrote and nothing runs under
//     it. The field table's own rule — a field no case reads does not belong in
//     the description — is what makes this a defect and not a style choice.
//
// Both tests accept either cure the delivery might choose, so each has a passing
// branch: run every case the description describes, or refuse the description
// before it runs one. What neither accepts is the third shape, which is what
// happens today: the case is gone, the suite is green, and nothing says so.

// TestEveryRefusalADescriptionMakesEitherRunsOrIsRefused describes the one
// command of the notes port as owing the unknown-row refusal from two different
// states, each with its own sentence, and asks the harness to account for both.
func TestEveryRefusalADescriptionMakesEitherRunsOrIsRefused(t *testing.T) {
	const (
		fromASealedNote = "File: an unknown row is not found on a sealed note"
		fromAFreshNote  = "File: an unknown row is not found on a fresh note"
	)
	suite := noteSuite(knobs{})
	unknown := suite.Ops[0].Refusals[0]
	if unknown.Kind != Unknown {
		t.Fatalf("the notes port's first refusal is %q, not the floor's unknown row; this pin needs rebasing", unknown.Kind)
	}
	first, second := unknown, unknown
	first.Name = fromAFreshNote
	second.Name = fromASealedNote
	// An honest fake: nothing here is broken, so a case that does not run cannot
	// be excused as a case that failed.
	suite.Ops[0].Refusals = append([]Refusal[world]{first, second}, suite.Ops[0].Refusals[1:]...)

	if problems := watch(t, suite).failures(""); len(problems) > 0 {
		for _, p := range problems {
			if strings.Contains(p, "unknown row") || strings.Contains(p, "same floor") {
				return // the floor refused the duplicate, which is a cure
			}
		}
		t.Fatalf("the description was refused for reasons that do not name the duplicate floor case: %v", problems)
	}

	ran := watch(t, suite).names()
	for _, want := range []string{fromAFreshNote, fromASealedNote} {
		if !slices.Contains(ran, want) {
			t.Errorf("%q was described and never ran; the suite ran %v. A refusal the module describes has to run, or the floor has to refuse the description and say which refusal duplicated a floor case — a case that vanishes from a green suite is the loss this package exists to make impossible", want, ran)
		}
	}
	// Names is documented as "every case name Run would run" and the pin a module
	// keeps is read through it, so a silently dropped refusal is invisible twice.
	if planned := Names(suite); slices.Contains(planned, fromASealedNote) != slices.Contains(ran, fromASealedNote) {
		t.Errorf("Names and Run disagree about %q: Names says %v, Run ran %v", fromASealedNote, planned, ran)
	}
}

// TestANameForACaseThatNeverRunsIsRefused puts the module's own sentence for the
// unknown-row refusal in Op.Names, keyed by the floor Kind the README table
// prints, and asks whether the harness reads it.
func TestANameForACaseThatNeverRunsIsRefused(t *testing.T) {
	const sentence = "File: nobody's note is nobody's note"
	suite := noteSuite(knobs{})
	for i, r := range suite.Ops[0].Refusals {
		if r.Kind == Unknown {
			r.Name = "" // the module's sentence is in Names, not on the refusal
			suite.Ops[0].Refusals[i] = r
		}
	}
	if suite.Ops[0].Names == nil {
		suite.Ops[0].Names = map[Kind]string{}
	}
	suite.Ops[0].Names[Unknown] = sentence

	tr := watch(t, suite)
	if problems := tr.failures(""); len(problems) > 0 {
		for _, p := range problems {
			if strings.Contains(p, sentence) || strings.Contains(p, "Names") {
				return // the floor refused a name it would never read, which is a cure
			}
		}
		t.Fatalf("the description was refused for reasons that do not name the unread name: %v", problems)
	}
	if !slices.Contains(tr.names(), sentence) {
		t.Errorf("the module named the unknown-row case %q in Op.Names and it ran under another name (%v); an evidence link points at the sentence the module wrote, so either the harness reads the name where the module wrote it or the floor refuses a Names key it never reads", sentence, tr.names())
	}
}
