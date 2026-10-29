package audit_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	kitoperation "github.com/septagon-oss/platformkit/kit/operation"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/pkit"
)

// TestTheTrailAcceptsAnOperationItCanAttribute asks the handler the composed plan
// carries for the trail about an operation it CAN attribute — a revision, a known
// outcome and two identifiers the kernel parses — and requires that it not be answered
// the way an unattributable operation is. The sibling cases hold each unattributable
// shape to one refusal sentence, and a recorder that refused every event with that
// sentence would answer them all correctly. This case is what makes that sentence mean
// the operation: a well-formed one gets past the metadata check and reaches the insert,
// which a fixture with no store cannot serve, so the answer here is a write being
// attempted rather than an operation being refused.
func TestTheTrailAcceptsAnOperationItCanAttribute(t *testing.T) {
	var wired []module.Module
	reader := pkit.NewModule("attributereader", func(w *pkit.Wiring) (module.Module, error) {
		wired = pkit.Composition(w)
		return module.Module{Name: "attributereader"}, nil
	}, pkit.After(pkit.Everything))
	if _, err := pkit.NewApp("attributable").Use(probeInputs, audit.Module, reader).
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
	op := kitoperation.Identity{
		ID: "op-attributable", Ref: uuid.NewString(), Tenant: uuid.NewString(),
		Outcome: kitoperation.Succeeded, Revision: 1,
	}
	// The zero transaction stands for the one a delivery carries and holds no handle
	// here, so the insert a valid operation reaches fails or panics on the missing
	// store. Either answer says the operation itself was accepted.
	var answer string
	func() {
		defer func() {
			if p := recover(); p != nil {
				answer = fmt.Sprintf("the write was attempted: %v", p)
			}
		}()
		if err := handler(context.Background(), db.Tx[db.Tenant]{}, events.Event{
			Name: "audit.test_recorded", Operation: op,
		}); err != nil {
			answer = err.Error()
		}
	}()
	if strings.Contains(answer, "operation metadata") {
		t.Errorf("the wired handler refused %q, an operation the kernel accepts, with %q: that refusal is the whole answer this recorder gives", op.ID, answer)
	}
}
