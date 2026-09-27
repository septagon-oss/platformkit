package porttest

import (
	"testing"
)

// Review 5. World.Refused makes four assertions about a refused call — the error
// is the refusal named, it is in the class named, nothing was written, nothing
// was said — and only the first three had a case in this package watching them
// fail. The class is what tells a caller whether to retype the request or stop
// asking, so a harness that stopped making that assertion would still report
// every refusal of every port green, and nothing in the three converted modules
// would notice: each names a class on most of its refusals.
//
// It bites today. Renaming one class and changing nothing else fails that one
// case and only that case; deleting the class branch from World.Refused reddens
// this file. Where the assertion is fragile is said plainly here because the
// review measured it: World.Refused asks it only when the refusal names a class
// *and* the suite's Classify is non-nil, so a suite that sets Classify to nil
// while its refusals go on naming classes loses it with no sentence anywhere —
// the same shape 32aee9b closed for an operation whose Call renders nothing, and
// the correction there (refuse the description up front) is the correction here.

func TestRefusalCaseFailsWhenTheClassIsMisnamed(t *testing.T) {
	suite := noteSuite(knobs{})
	suite.Own = nil
	op := suite.Ops[0]
	named := false
	for i, r := range op.Refusals {
		if r.Kind == Stale {
			r.Class = Immutable // the port's own classifier calls a moved revision Correctable
			op.Refusals[i], named = r, true
		}
	}
	if !named {
		t.Fatal("the example port no longer describes a Stale refusal, so this pin has no subject")
	}
	suite.Ops[0] = op

	log := watch(t, suite)
	name := "File: a revision the row is not at is refused and writes nothing"
	log.mustFail(t, name, "correctable")
	log.mustPassApartFrom(t, name)
}

// TestRefusalCaseAssertsTheClassThePortGives is the same assertion pointed the
// other way: the class the port's classifier gives is accepted, so the case
// cannot pass by refusing every classification out of hand.
func TestRefusalCaseAssertsTheClassThePortGives(t *testing.T) {
	log := watch(t, noteSuite(knobs{}))
	for _, name := range []string{
		"File: a revision the row is not at is refused and writes nothing", // Correctable
		"a sealed note is not filed twice",                                 // Immutable
	} {
		if f := log.failures(name); len(f) > 0 {
			t.Errorf("%q failed although the description names the class the port gives: %v", name, f)
		}
	}
}
