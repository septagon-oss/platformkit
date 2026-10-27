package audit_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/pkit"
)

// TestTheWalksRefusalStandsAloneInEveryDeclarationOrder plans the walk with no trail in
// both orders its two modules can be declared in and reads the refusal whole rather than
// one problem of it: every problem must be the walk's own build refusing, attributed to
// the walk, and one of them must name the module whose part never arrived. A plan that
// stopped for the trail and for a second reason as well would leave the cases beside this
// one passing on a composition that also failed for reasons of their own, and an order
// answered by another module's refusal would mean the trail's arrival turned on position.
func TestTheWalksRefusalStandsAloneInEveryDeclarationOrder(t *testing.T) {
	for _, order := range [][]*pkit.Module{
		{probeInputs, probeTaker()},
		{probeTaker(), probeInputs},
	} {
		names := ""
		for _, m := range order {
			names += " " + m.Name()
		}
		_, err := pkit.NewApp("alone").Use(order...).Plan(pkit.Deployment{Environment: pkit.Development})
		if err == nil {
			t.Fatalf("Use(%s): a walk composed with no trail answered no refusal", names)
		}
		var refusal *pkit.CompositionError
		if !errors.As(err, &refusal) {
			t.Fatalf("Use(%s): the refusal is a %T, not the composition's own: %v", names, err, err)
		}
		walks := 0
		for _, problem := range refusal.Problems {
			if problem.Cause != "Build" || problem.Module != "probetaker" || !strings.Contains(problem.Sentence, "audit") {
				t.Errorf("Use(%s): the refusal carries %s refusing for %s rather than the walk's own build: %q",
					names, problem.Module, problem.Cause, problem.Sentence)
				continue
			}
			walks++
		}
		if walks == 0 {
			t.Errorf("Use(%s): no problem is the walk's own build refusing for want of the trail: %+v", names, refusal.Problems)
		}
	}
}
