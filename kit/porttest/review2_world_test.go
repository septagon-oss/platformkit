package porttest

import (
	"strings"
	"testing"
)

// Review 2, finding 3 of review 1, the shape its own test did not run. Review 1
// proved the all-or-nothing case: a World that never calls the closure reported
// all eight cases green. The cure records one bool per case, so the realistic
// shape — a transaction-shaped closure that opens for most cases and returns
// early for one of them, which is what an error path in a fixture looks like —
// must fail exactly that case and leave the others alone.
//
// The floor this pins: a case counts as run only if the world ran *that* case,
// not if the world ran some case. It passes while the bool is per case; it fails
// if it is ever hoisted out of the loop or made a property of the suite.
func TestAWorldThatSkipsOneCaseFailsOnlyThatCase(t *testing.T) {
	names := Names(noteSuite(knobs{}))
	if len(names) < 2 {
		t.Fatalf("the reference description plans %d cases; this test needs at least two", len(names))
	}
	skipped := names[len(names)-1] // the last one, so the early return is not the first thing the world does

	// Reachability, through the fixed behaviour: with a world that runs every
	// case, this description is green, so a failure below is the skip and not
	// the fake.
	if log := watch(t, noteSuite(knobs{})); len(log.failures("")) > 0 {
		t.Fatalf("the reference description was refused: %v", log.failures(""))
	}

	suite := noteSuite(knobs{})
	full := suite.World
	var at int
	suite.World = func(t *testing.T, run func(world)) {
		this := names[at]
		at++
		if this == skipped {
			return // the fixture's error path: opened nothing, ran nothing
		}
		full(t, run)
	}

	log := watch(t, suite)
	for _, name := range log.names() {
		failures := log.failures(name)
		if name == skipped {
			if len(failures) == 0 {
				t.Errorf("%q reported green under a world that never ran it; a case that ran against nothing "+
					"asserts nothing about the port", name)
				continue
			}
			if !strings.Contains(strings.Join(failures, "\n"), "the world returned without running this case") {
				t.Errorf("%q failed with %v, and none of it says the world never ran it", name, failures)
			}
			continue
		}
		if len(failures) > 0 {
			t.Errorf("%q failed too (%v); one case the world skipped is one case that fails", name, failures)
		}
	}
}
