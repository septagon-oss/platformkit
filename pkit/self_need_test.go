package pkit_test

// A module that needs the contract it provides itself is a cycle of one: no
// order builds the value before the build that reads it. The resolver skips a
// self-edge when it orders, so the composition is accepted and the build
// reads a value that does not exist yet. The composition has to answer this
// with a sentence naming the module, the way it answers a cycle of two.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
)

func TestAModuleThatNeedsWhatItProvidesIsRefused(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Validate panicked instead of refusing: %v", r)
		}
	}()
	selfish := pkit.NewModule("selfish", func(w *pkit.Wiring) (module.Module, error) {
		_ = pkit.Get[cartcontracts.Service](w)
		pkit.Put(w, cartcontracts.Service(nil))
		return module.Module{Name: "selfish"}, nil
	}, pkit.Provides[cartcontracts.Service](), pkit.Needs[cartcontracts.Service]())

	app := pkit.NewApp("collect").Use(selfish)
	err := app.Validate(dev)
	if err == nil {
		t.Fatal("a module that needs what it provides itself was accepted")
	}
	if !strings.Contains(err.Error(), "selfish") {
		t.Errorf("the refusal does not name the module: %v", err)
	}
	if _, err := app.Explain(dev); err == nil {
		t.Error("Explain answered a composition that does not resolve")
	}
}
