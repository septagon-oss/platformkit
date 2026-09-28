package porttest

import (
	"slices"
	"testing"
)

// Review 10. README promises "Names and Run read the same list, so a pinned name list
// is the list that runs", and the whole reason a module may pin a list instead of
// running it is that promise. Review 9 found a case dropped from both lists at once —
// an agreement that hid a loss — so the two agreeing is not the whole guarantee, yet
// nothing in this package asserted the agreement itself across a description that opens
// every naming door at once. Each door has a witness of its own
// (`TestRunRunsEveryRefusalADescriptionMakes` for a second refusal of one Kind,
// `TestAFloorRefusalNamedByItsOperationRunsUnderThatName` for a sentence filed under a
// Kind, `TestRunRefusesANameNoCaseRunsUnder` for a sentence that reaches no case,
// `TestNamesAreUnchanged` for this package's own list), so a rename that landed in
// `caseName` or `refusalName` but not in `plan` would have to break two of them before
// anybody saw it, and a module's pinned list — the thing a client's requirements index
// links at — would be the file that was wrong.

const (
	firstUnknownMissing  = "File: nobody's note is nobody's note"
	secondUnknownMissing = "File: a missing note is a missing note"
	byAMember            = "File: a member says what they filed"
	twiceFilesOnce       = "File: filing it twice files it once"
	strangerRefused      = "File: a stranger cannot file it"
)

// TestTheNameListIsTheListThatRuns is the promise, whole: for one description that uses
// every way a case gets a name at the same time — a sentence for the success, one for
// the retry, one for a floor refusal that carries no sentence of its own, two named
// refusals of one floor Kind, a refusal of the module's own typed ahead of the floor
// ones, and a hand-written case — the ordered list the harness ran *is* the list
// `Names` printed, each name exactly once.
func TestTheNameListIsTheListThatRuns(t *testing.T) {
	suite := noteSuite(knobs{})
	// Two refusals of one floor Kind, each with a sentence of its own — the shape a
	// module writes when it owes one refusal described twice. The base unknown-row
	// refusal is copied, so both entries ask the same call and only the sentence
	// differs; the copy is typed first, so the order the cases run in is the floor's
	// and not the order they were typed in.
	second, first := suite.Ops[0].Refusals[0], suite.Ops[0].Refusals[0]
	first.Name, second.Name = firstUnknownMissing, secondUnknownMissing
	own := suite.Ops[0].Refusals[4] // "a sealed note is not filed twice"
	suite.Ops[0].Refusals = append([]Refusal[world]{second, own, first}, suite.Ops[0].Refusals[1:4]...)
	suite.Ops[0].Names = map[Kind]string{Success: byAMember, Retry: twiceFilesOnce, Denied: strangerRefused}

	// Reachability through the fixed behaviour: every door above is open in the list
	// the harness prints, so the equality below compares nine named cases and not two
	// empties, and a test that only watched the equality could not pass on a
	// description that generated nothing.
	want := Names(suite)
	for _, door := range []string{byAMember, twiceFilesOnce, strangerRefused,
		firstUnknownMissing, secondUnknownMissing,
		"File: a revision the row is not at is refused and writes nothing",
		"File: another tenant cannot reach the row",
		"a sealed note is not filed twice",
		"the note carries the day it was filed"} {
		if !slices.Contains(want, door) {
			t.Fatalf("Names does not even print %q, so the equality below would assert nothing: %v", door, want)
		}
	}
	if len(want) != 9 {
		t.Fatalf("Names prints %d cases, want the nine this description generates: %v", len(want), want)
	}

	log := watch(t, suite)
	log.mustPassApartFrom(t)
	if ran := log.names(); !slices.Equal(ran, want) {
		t.Errorf("the suite ran %v and Names printed %v; a module pins the second and a requirements index links at it, while the first is what was tested", ran, want)
	}
	seen := map[string]int{}
	for _, name := range log.names() {
		seen[name]++
	}
	for name, times := range seen {
		if times > 1 {
			t.Errorf("%q ran %d times under one name; an index that points at it cannot say which run tested it", name, times)
		}
	}
}

// TestTwoUnnamedRefusalsOfOneKindShareNoSentence is the seam between the two rules the
// tip added: every refusal a description makes is a case, *and* a sentence an operation
// files under a Kind names the case of that Kind. A refusal with no sentence of its own
// takes the operation's, so two refusals of one Kind with no sentence of their own would
// both take it — two cases under one name, which is what a requirements index cannot
// point at. The floor refuses the description; it does not run one under a borrowed name
// and lose the other.
func TestTwoUnnamedRefusalsOfOneKindShareNoSentence(t *testing.T) {
	suite := noteSuite(knobs{})
	suite.Ops[0].Names = map[Kind]string{Unknown: "File: nobody's note is nobody's note"}
	suite.Ops[0].Refusals = append([]Refusal[world]{suite.Ops[0].Refusals[0]}, suite.Ops[0].Refusals...)
	log := watch(t, suite)
	if ran := log.names(); len(ran) > 0 {
		t.Errorf("the description ran %v; two refusals of one Kind both read Op.Names, so they are two cases under one name and the floor has to refuse rather than run one under a borrowed sentence", ran)
		return
	}
	log.mustRefuseTheSuite(t, "two cases would run under the name")
}

// TestAModuleOwnRefusalNeverRunsBeforeTheFloor closes the order half of the same promise
// from the other side. A description that types its named refusal first still runs the
// floor first, and both lists say so. Walking the literal list once — the shape a future
// simplification of `plan` takes, and the shape `plan` had for the module's own refusals
// before the tip — would run the module's own refusal ahead of the unknown-row case and
// `Names` would print that order too: the pinned list moves in a diff, every client
// evidence link that quotes a position moves with it, and no assertion is lost to show
// anybody.
func TestAModulesOwnRefusalNeverRunsBeforeTheFloor(t *testing.T) {
	suite := noteSuite(knobs{})
	own := suite.Ops[0].Refusals[4]
	suite.Ops[0].Refusals = append([]Refusal[world]{own}, suite.Ops[0].Refusals[:4]...)
	log := watch(t, suite)
	log.mustPassApartFrom(t)
	ran := log.names()
	if len(ran) == 0 || ran[0] != "File: the operation says what it did" {
		t.Fatalf("the suite ran %v; the success is the first case of an operation whatever order its refusals were typed in", ran)
	}
	if ownAt, tenantAt := slices.Index(ran, "a sealed note is not filed twice"), slices.Index(ran, "File: another tenant cannot reach the row"); ownAt < tenantAt {
		t.Errorf("the module's own refusal ran at %d and the floor's tenant case at %d; the floor runs first and then the module's own: %v", ownAt, tenantAt, ran)
	}
	if !slices.Equal(ran, Names(suite)) {
		t.Errorf("Run ran %v and Names printed %v", ran, Names(suite))
	}
}
