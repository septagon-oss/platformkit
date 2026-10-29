package audit_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/audit"
)

// TestTheTrailGoesByOneNameEverywhereItNamesItself asks the trail to name itself one
// way: the name the composition declares, the name the manifest it builds carries, and
// the name its event subscription is attributed to are one name, read from the
// declaration rather than repeated as a literal here. kit/module compares the
// manifest's copy with the subscription's copy and nothing compares either with the
// declaration; the subscription's copy names the durable consumer that carries the
// trail's deliveries, so a pair that moved together would move the trail's rows, its
// consumer and its retention sweep to a module no plan declares.
func TestTheTrailGoesByOneNameEverywhereItNamesItself(t *testing.T) {
	declared := audit.Module.Name()
	trail, err := audit.NewChecked(audit.Deps{})
	if err != nil {
		t.Fatalf("the trail built from its defaults: %v", err)
	}
	if trail.Name != declared {
		t.Errorf("the manifest names itself %q while the composition declares %q: one module names itself once", trail.Name, declared)
	}
	if len(trail.Subscriptions) == 0 {
		t.Fatalf("the trail declares no subscription, so nothing here answers who its deliveries belong to")
	}
	for _, sub := range trail.Subscriptions {
		if sub.Module != declared {
			t.Errorf("the trail's subscription is attributed to %q while the composition declares %q: a subscription's module names the durable consumer its deliveries arrive on", sub.Module, declared)
		}
	}
}
