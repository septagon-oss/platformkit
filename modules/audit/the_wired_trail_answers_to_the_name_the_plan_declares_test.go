package audit_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/pkit"
)

// TestTheWiredTrailAnswersToTheNameThePlanDeclares asks the trail an application
// composes — the manifest provider.go's wire hands back, not the one NewChecked
// answers to a hand-built Deps — to answer to the name the plan declares, and to
// hold its handler and its sweep under that name. Those two names are keys: the
// subscription's names the durable consumer the trail's deliveries arrive on, and a
// job's is the advisory lock that keeps two replicas from sweeping one tenant twice.
// kit/module refuses a manifest whose subscription is attributed elsewhere, so a name
// moved alone dies at the plan; a pair that moved together, or a sweep renamed while
// the module stayed, reaches the plan clean.
func TestTheWiredTrailAnswersToTheNameThePlanDeclares(t *testing.T) {
	declared := audit.Module.Name()
	var wired []module.Module
	reader := pkit.NewModule("wiredtrailreader", func(w *pkit.Wiring) (module.Module, error) {
		wired = pkit.Composition(w)
		return module.Module{Name: "wiredtrailreader"}, nil
	}, pkit.After(pkit.Everything))
	if _, err := pkit.NewApp("wired").Use(probeInputs, audit.Module, reader).
		Plan(pkit.Deployment{Environment: pkit.Development}); err != nil {
		t.Fatalf("the plan refused: %v", err)
	}
	for _, m := range wired {
		if m.Name != declared {
			continue
		}
		if !m.SubscribeAll || len(m.Subscriptions) != 1 || m.Subscriptions[0].Module != declared {
			t.Errorf("the wired trail subscribes %d handler(s) with SubscribeAll %v, want the one handler attributed to %q",
				len(m.Subscriptions), m.SubscribeAll, declared)
		}
		if len(m.Jobs) == 0 {
			t.Fatalf("the wired trail declares no sweep, so nothing expires the rows its handler writes")
		}
		for _, j := range m.Jobs {
			if !strings.HasPrefix(j.Name, declared+"-") {
				t.Errorf("the wired trail schedules %q, whose advisory lock is named for nobody this plan declares (%q)", j.Name, declared)
			}
		}
		return
	}
	t.Errorf("no composed manifest answers to %q, the name the trail's module declares", declared)
}
