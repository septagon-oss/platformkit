package porttest

// Review 12. One floor refusal, and this is the sentence that keeps it.
//
// `Op.problems` refuses two of an operation's refusals carrying one `Name`, and
// nothing in this repository or in the three converted suites said so: deleting
// that refusal leaves kit/porttest, tasktest, contenttest and sitetest green,
// which round 19 measured and this round re-measured by running the mutant
// rather than by reading the list.
//
// The refusal is not the only thing standing between a shared name and a lost
// case — two refusals under one sentence also plan two cases under one name, and
// `Suite.problems` refuses that collision, which TestRunRunsEveryRefusalADescriptionMakes
// already watches. What only this sentence carries is *which field to edit*: the
// collision names the sentence and points at "one of the two cases has to move",
// while this one names the operation whose refusals collided. A module author
// handed the first sentence alone has to open the description and find the pair;
// a requirements index in a client repository is why the pair matters at all.
//
// So the witness asks for both halves: the description refused, no case run, and
// the refusal saying which operation owed it and which sentence it was shared by.

import (
	"strings"
	"testing"
)

// TestRunRefusesTwoRefusalsOfOneOperationSharingAName gives the operation a second
// refusal of its own with the sentence the first one already carries.
func TestRunRefusesTwoRefusalsOfOneOperationSharingAName(t *testing.T) {
	const shared = "a sealed note is not filed twice"
	suite := noteSuite(knobs{})
	sealed := suite.Ops[0].Refusals[len(suite.Ops[0].Refusals)-1]
	if sealed.Name != shared {
		t.Fatalf("the note port's own refusal is %q; this witness is written against %q", sealed.Name, shared)
	}
	// The same refusal twice is the shape a module writes by accident: the second
	// entry differs from the first in nothing but its position, and it is the
	// position a case name cannot tell.
	suite.Ops[0].Refusals = append(suite.Ops[0].Refusals, sealed)

	log := watch(t, suite)
	log.mustRefuseTheSuite(t, `two refusals named "`+shared+`"`)
	if said := strings.Join(log.failures(""), "\n"); !strings.Contains(said, "File") {
		t.Errorf("the refusal says which refusals share the sentence but not whose they are; it says: %v", said)
	}
}
