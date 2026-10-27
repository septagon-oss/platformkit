package tour_test

// Both passes are pure functions over a declaration, so a name an author wrote
// badly has to come back as a sentence about their own tour rather than as a
// panic or as silence. kit/app prints every problem of every module in one
// report, so a refusal that does not say whose tour it refuses cannot be acted
// on. The battery walks the hostile end of each name a step or a trigger carries.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

// hostileNames are the spellings a tour can reach: empty, a bare separator, a
// control character, a bad escape, a wrong case, a path, and names past any
// length a declaration would want.
func hostileNames() []string {
	return []string{"", "#", "##", "app/feature/custom/editor#", "a#b", "\x00", "\n", "%zz/app",
		"APP/feature/custom/editor#action.submit", "../../etc/passwd",
		strings.Repeat("x", 4096), "app/feature/custom/editor#" + strings.Repeat("y", 1024)}
}

// hostileDeclarations says each name in each of the four places a tour may say
// it: a modal's view, an anchored step's control, a completion's operation and an
// automatic trigger's view.
func hostileDeclarations() []tour.Tour {
	content := tour.Content{Title: text("feature.m", "Look here"), Body: text("feature.m.body", "Nothing more.")}
	control := screen.AnchorRef("app/feature/custom/editor#action.submit")
	var built []tour.Tour
	for _, name := range hostileNames() {
		steps := []tour.Step{
			tour.Modal("m", screen.ViewRef(name), content),
			tour.Spotlight("a", screen.AnchorRef(name), content, tour.Next(), tour.AnchoredOptions{}),
			tour.Spotlight("c", control, content, tour.Completes(tour.OperationRef(name)), tour.AnchoredOptions{}),
		}
		for i := range steps {
			step := steps[i]
			for _, automatic := range []bool{false, true} {
				declared := tour.Tour{
					Ref:   tour.Ref{ID: "feature.first", Edition: 1},
					Title: text("feature.first", "Add your first note"),
					Steps: []tour.Step{step},
				}
				if automatic {
					start := tour.OnVisit(screen.ViewRef(name))
					declared.Automatic = &start
				}
				built = append(built, declared)
			}
		}
	}
	return built
}

func TestEveryTourRefusalNamesTheTourItRefuses(t *testing.T) {
	refused := 0
	for _, declared := range hostileDeclarations() {
		problems := append(declared.DeclarationProblems("feature"), declared.CompositionProblems(composition())...)
		refused += len(problems)
		for _, problem := range problems {
			if !strings.HasPrefix(problem, `tour "feature.first"`) {
				t.Errorf("refusal %q does not name the tour it refuses", problem)
			}
		}
	}
	if refused == 0 {
		t.Fatal("the battery refused nothing, so it asked nothing of either pass")
	}
}
