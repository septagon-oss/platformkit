package porttest

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

// Review 6. Two of the harness's own comparisons had no witness: no test asked
// whether the case fails when the thing they exist to catch happens. Every
// shipped port answers its refusals with the right error and publishes what it
// says it publishes, so both comparisons are green whatever the harness does
// with them — measured at 3faab05, deleting World.Refused's `case !is(err)`
// branch, and deleting Suite.success's call to published, each left kit/porttest
// and the three converted suites reporting ok. Both work today (a fake refused
// with the wrong sentinel is refused back by the shipped harness — see
// TestFakeConforms in modules/task — and so is one that files a note without
// saying so). What these two pins keep is the *comparison*: they pass when the
// harness says so and fail the moment either branch is deleted, which is the
// diff that would otherwise arrive as green.
//
// Each has the passing branch the defect leaves open: with the refusal named and
// the events as described, TestRunPassesAFakeThatIsRight runs both cases green.

func TestRefusalCaseFailsWhenTheErrorIsNotTheRefusalItNames(t *testing.T) {
	suite := noteSuite(knobs{})
	suite.Own = nil
	// The denial's call, answered with some other refusal. The port still
	// refuses, writes nothing and says nothing, so this case is left holding one
	// assertion only: whether the error is the refusal the case names. Nothing
	// else in the harness can tell two refusals apart — the class is the same for
	// both, and the snapshot and the events move for neither.
	for i, r := range suite.Ops[0].Refusals {
		if r.Kind == Denied {
			suite.Ops[0].Refusals[i].Call = func(world, uuid.UUID) error {
				return errors.New("notes: something else refused it")
			}
		}
	}
	log := watch(t, suite)
	const denied = "File: a caller with no grant is refused and writes nothing"
	log.mustFail(t, denied, "is not the refusal this case names")
	log.mustPassApartFrom(t, denied)
}

func TestSuccessCaseFailsWhenThePortSaidNothingAboutWhatItPublished(t *testing.T) {
	// The description and the port disagree, both ways round: a port that says
	// the success case publishes nothing while it published an event, and one
	// that names an event twice. The success case is the only place the harness
	// asks what a working call said, so an assertion lost here is lost for the
	// whole port — the retry asks only that the second call says nothing.
	for _, said := range [][]string{nil, {"note.filed", "note.filed"}} {
		suite := noteSuite(knobs{})
		suite.Own = nil
		suite.Ops[0].Publishes = said
		log := watch(t, suite)
		const success = "File: the operation says what it did"
		log.mustFail(t, success, "published")
		log.mustPassApartFrom(t, success)
	}
}
