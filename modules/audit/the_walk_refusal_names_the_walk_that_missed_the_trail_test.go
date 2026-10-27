package audit_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/pkit"
)

// TestTheWalkRefusalNamesTheWalkThatMissedTheTrail reads the refusal a plan answers
// with no trail as the composition's own value rather than as its sentence: the walk's
// build must be the thing that refused, and it must name the module whose part it never
// received. A plan that stopped for any other reason prints a sentence naming audit too —
// a contribution declared and not put, a need with no supplier — and the cases beside
// this one are worth only as much as the refusal they lean on.
func TestTheWalkRefusalNamesTheWalkThatMissedTheTrail(t *testing.T) {
	_, err := pkit.NewApp("absent").Use(probeInputs, probeTaker()).
		Plan(pkit.Deployment{Environment: pkit.Development})
	if err == nil {
		t.Fatal("a walk composed with no trail answered no refusal")
	}
	var refusal *pkit.CompositionError
	if !errors.As(err, &refusal) {
		t.Fatalf("the refusal is a %T, not the composition's own: %v", err, err)
	}
	for _, problem := range refusal.Problems {
		if problem.Module == "probetaker" && problem.Cause == "Build" && strings.Contains(problem.Sentence, "audit") {
			return
		}
	}
	t.Errorf("no problem is the walk's own build refusing for want of the trail: %+v", refusal.Problems)
}
