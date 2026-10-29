package audit_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/pkit"
)

// TestTheTrailNamesItsPersonalDataPartForTheModuleItIs asks the personal-data part the
// trail contributes to carry the name the composition declares for the trail. The
// erasure walk keys a person's bundle by that name, so a part named for anything else
// hands a person's audit rows to a module nobody declared, and the cases beside this
// one pass on it: they read the name the module reads, never the name it declares.
func TestTheTrailNamesItsPersonalDataPartForTheModuleItIs(t *testing.T) {
	declared := audit.Module.Name()
	named := 0
	walk := pkit.NewModule("namingwalk", func(w *pkit.Wiring) (module.Module, error) {
		for _, part := range pkit.All[personaldata.Part](w) {
			if part.Module == declared {
				named++
			}
		}
		return module.Module{Name: "namingwalk"}, nil
	}, pkit.Needs[[]personaldata.Part]())
	if _, err := pkit.NewApp("names").Use(probeInputs, audit.Module, walk).
		Plan(pkit.Deployment{Environment: pkit.Development}); err != nil {
		t.Fatalf("the plan refused: %v", err)
	}
	if named != 1 {
		t.Errorf("%d personal-data parts carry %q, the name the trail's module declares: the part and the declaration are one fact, not two literals", named, declared)
	}
}
