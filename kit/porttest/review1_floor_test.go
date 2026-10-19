package porttest

import "testing"

// Review 1, findings 2 and 3. porttest.go's own words for problems() are "house
// rule 8 made mechanical: the write that takes the last assertion away is
// refused, and a port that genuinely has no such case writes the reason down and
// is accepted." Two writes take every assertion away and are accepted in
// silence.

// A description with no operation and no case of its own is accepted and reports
// green having asserted nothing. Deleting the last Op from a converted suite —
// or writing a new one and forgetting to fill Ops in — leaves RunService passing
// for every implementation of the port, which is the one thing a conformance
// suite may never do.
//
// The floor this pins: a suite that would run no case is refused before it runs
// one, the way an Own case with no Because already is. It passes as soon as
// problems() says so.
func TestRunRefusesADescriptionThatRunsNoCase(t *testing.T) {
	empty := Suite[world]{
		Port:   "contracts.Nothing",
		World:  noteWorld(knobs{}),
		Events: func(w world) []string { return w.service.Events.Names() },
	}
	// Reachability, through the fixed behaviour: the same world, with the port's
	// operations in it, plans the eight cases this package pins.
	if planned := Names(noteSuite(knobs{})); len(planned) == 0 {
		t.Fatalf("the reference description plans no case either; this test cannot tell the two apart")
	}
	log := watch(t, empty)
	if ran := log.names(); len(ran) > 0 {
		t.Fatalf("a description with no Ops and no Own ran %v", ran)
	}
	if refused := log.failures(""); len(refused) == 0 {
		t.Errorf("a description with no Ops and no Own was accepted and reported green; " +
			"a suite that asserts nothing is the write that takes the last assertion away")
	}
}

// A World that never calls the case it was handed reports every generated case
// as passed — the success, the retry and all four refusals, tenancy among them —
// with no implementation touched. Run already refuses the neighbouring mistake
// (a world that hands back a zero fixture), and a module's World is a
// transaction-shaped closure with early returns in it.
//
// The floor this pins: a case counts as run only if the world ran it. It passes
// as soon as Run says so, which is one bool per case.
func TestRunRefusesAWorldThatNeverRunsTheCase(t *testing.T) {
	suite := noteSuite(knobs{})
	suite.World = func(t *testing.T, run func(world)) {} // the case is never called

	// Reachability, through the fixed behaviour: with a world that does call the
	// case, this description passes, so what the assertion below reads is the
	// world and not a broken fake.
	if log := watch(t, noteSuite(knobs{})); len(log.failures("")) > 0 {
		t.Fatalf("the reference description was refused: %v", log.failures(""))
	}

	log := watch(t, suite)
	var green []string
	for _, name := range log.names() {
		if len(log.failures(name)) == 0 {
			green = append(green, name)
		}
	}
	if len(green) > 0 {
		t.Errorf("%d of %d cases reported green under a world that never ran one of them: %v",
			len(green), len(log.names()), green)
	}
}
