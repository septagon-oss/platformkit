package pkit_test

// The sentence a refused build reaches its caller with.
//
// A module whose build returned an error put none of what it declared, so every
// module the resolver placed after it would read the zero value of a contract that
// was declared and never supplied. A module that guards its own Deps answers the
// deployment's own mistake — a store it named an address for and never finished
// configuring — with its own sentence, through the build's error rather than as a
// panic the caller could only recover. The refusal under test is the one the caller
// has to hear: the module that refused, by its own name, with nothing downstream of
// it run at all.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestAModuleDownstreamOfARefusedBuildIsNotBuilt(t *testing.T) {
	supplier := pkit.NewModule("supplier", func(w *pkit.Wiring) (module.Module, error) {
		return module.Module{}, errBoom
	}, pkit.Provides[unsuppliedPort]())
	taker := pkit.NewModule("taker", func(w *pkit.Wiring) (module.Module, error) {
		if pkit.Get[unsuppliedPort](w) == nil {
			panic("taker: Deps.Supplier is required")
		}
		return module.Module{Name: "taker"}, nil
	}, pkit.Needs[unsuppliedPort]())

	a := pkit.NewApp("collect").Use(supplier, taker)
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("the refused provider reached its caller as a panic: %v", recovered)
		}
	}()
	err := a.Validate(dev)
	if err == nil {
		t.Fatal("a composition with a provider that refused resolved")
	}
	if !strings.Contains(err.Error(), "supplier: boom") {
		t.Errorf("the refusal is not the refusing module's own sentence: %v", err)
	}
	if strings.Contains(err.Error(), "taker") {
		t.Errorf("the follow-on problem is answered beside the cause, so one mistake is two sentences: %v", err)
	}
}

// unsuppliedPort is the contract the refusing module declared and never put.
type unsuppliedPort interface{ Supply() }
