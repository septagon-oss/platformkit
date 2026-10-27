package audit_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/pkit"
)

// TestTheTrailReachesTheWalkInEveryOrderThePlanDeclaresItsModulesIn plans the same
// three modules in every order they can be declared in. A contribution that arrived
// only in one textual order would be discovery by position rather than a declaration,
// and a deployment that lists its modules differently would hand the erasure walk a
// person's bundle with the trail missing from it.
func TestTheTrailReachesTheWalkInEveryOrderThePlanDeclaresItsModulesIn(t *testing.T) {
	for _, order := range [][]*pkit.Module{
		{probeInputs, audit.Module, probeTaker()},
		{probeInputs, probeTaker(), audit.Module},
		{probeTaker(), probeInputs, audit.Module},
		{probeTaker(), audit.Module, probeInputs},
		{audit.Module, probeInputs, probeTaker()},
		{audit.Module, probeTaker(), probeInputs},
	} {
		app := pkit.NewApp("order")
		names := ""
		for _, m := range order {
			app.Use(m)
			names += " " + m.Name()
		}
		if _, err := app.Plan(pkit.Deployment{Environment: pkit.Development}); err != nil {
			t.Errorf("Use(%s): %v", names, err)
		}
	}
}

// TestAWalkWithoutTheTrailAnswersItsRefusal checks the pin beside the trail: the taker
// refuses when no part named audit reached it, so the case above passes because the
// trail arrived and not because the walk asks nothing.
func TestAWalkWithoutTheTrailAnswersItsRefusal(t *testing.T) {
	_, err := pkit.NewApp("nowhere").Use(probeInputs, probeTaker()).
		Plan(pkit.Deployment{Environment: pkit.Development})
	if err == nil {
		t.Fatal("a plan with no trail answered no refusal")
	}
	if !strings.Contains(err.Error(), "audit") {
		t.Errorf("the refusal does not name the absent part: %v", err)
	}
}
