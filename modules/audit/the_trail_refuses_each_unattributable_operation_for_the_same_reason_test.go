package audit_test

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	kitoperation "github.com/septagon-oss/platformkit/kit/operation"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/pkit"
)

// TestTheTrailRefusesEachUnattributableOperationForTheSameReason asks the handler the
// composed plan carries for the trail to refuse two operations it cannot attribute — an
// admitted intent with no revision, and one whose result no worker would accept — and to
// answer each the same way however often it is asked. Each is refused by a different
// check the kernel makes before it inspects any single member, so neither answer can
// depend on which member a map reaches first, and the assertion holds the whole answer
// to one string rather than to a wording. The zero transaction stands for the one a
// delivery carries: the recorder answers an operation it cannot attribute before it
// reads or writes through one.
func TestTheTrailRefusesEachUnattributableOperationForTheSameReason(t *testing.T) {
	var wired []module.Module
	reader := pkit.NewModule("reasonreader", func(w *pkit.Wiring) (module.Module, error) {
		wired = pkit.Composition(w)
		return module.Module{Name: "reasonreader"}, nil
	}, pkit.After(pkit.Everything))
	if _, err := pkit.NewApp("reasons").Use(probeInputs, audit.Module, reader).
		Plan(pkit.Deployment{Environment: pkit.Development}); err != nil {
		t.Fatalf("the plan refused: %v", err)
	}
	var handler events.Handler
	for _, m := range wired {
		if m.Name == audit.Module.Name() && len(m.Subscriptions) == 1 {
			handler = m.Subscriptions[0].Handler
		}
	}
	if handler == nil {
		t.Fatal("no composed manifest of the trail carries one subscription handler")
	}
	for _, op := range []kitoperation.Identity{{ID: "op-1"}, {ID: "op-2", Outcome: "undecided", Revision: 1}} {
		var refusal string
		for range 12 {
			err := handler(context.Background(), db.Tx[db.Tenant]{}, events.Event{
				Name:      "audit.test_recorded",
				Operation: op,
			})
			switch {
			case err == nil:
				t.Fatalf("the wired handler accepted %s, an operation the kernel cannot attribute", op.ID)
			case !strings.Contains(err.Error(), "operation metadata"):
				t.Fatalf("the wired handler refused %s with %q, which names no refusal of the operation itself", op.ID, err)
			case refusal != "" && refusal != err.Error():
				t.Fatalf("the wired handler answered %s once with %q and once with %q", op.ID, refusal, err)
			}
			refusal = err.Error()
		}
	}
}
