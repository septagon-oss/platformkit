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

// TestTheHandlerThePlanCarriesRefusesAnOperationItCannotAttribute asks what the name
// cases leave open: whether the function the composed plan carries as the trail's
// handler is the trail's own recorder. The names are keys — the consumer deliveries
// arrive on, the lock the sweep takes — and the handler is the work. A wire that keeps
// every name and hands the plan a handler that answers nil writes no trail for any
// event while every name still agrees with itself, so this calls the handler the plan
// carries with an operation the kernel cannot attribute: the recorder refuses it
// before it reads the transaction, which is an answer a swallowed delivery never gives.
func TestTheHandlerThePlanCarriesRefusesAnOperationItCannotAttribute(t *testing.T) {
	var wired []module.Module
	reader := pkit.NewModule("handlerreader", func(w *pkit.Wiring) (module.Module, error) {
		wired = pkit.Composition(w)
		return module.Module{Name: "handlerreader"}, nil
	}, pkit.After(pkit.Everything))
	if _, err := pkit.NewApp("handler").Use(probeInputs, audit.Module, reader).
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
	// An intent with no revision is one the kernel refuses outright, so the recorder
	// answers it without reading the transaction, and the zero transaction here is
	// never asked to write a row. The revision is what is wrong rather than a member
	// the kernel validates in a map: which of two malformed members it names first is
	// that map's order, so a fixture built on one would answer differently run to run.
	err := handler(context.Background(), db.Tx[db.Tenant]{}, events.Event{
		Name:      "audit.test_recorded",
		Operation: kitoperation.Identity{ID: "op-1"},
	})
	if err == nil {
		t.Fatal("the wired handler accepted an operation the kernel refuses, so the plan carries no recorder behind the names")
	}
	if !strings.Contains(err.Error(), "operation metadata") {
		t.Errorf("the wired handler refused the unattributable operation with %q, which names no refusal of the operation itself", err)
	}
}
