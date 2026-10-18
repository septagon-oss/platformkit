package pkit_test

// The order's "no build order places it" answer is the backstop for a module
// silently dropped from a composition, and it is honest — but it is the generic
// sentence, and the composition file a client commits should say which rule the
// composition broke. Both of the refusals this round added name 0074's rows
// where they collide, and this pins the wording: with the specific refusal
// disabled the cases still fail through the backstop, so without a pin of its
// own the good sentence could rot into the generic one unnoticed.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
)

func TestTheTwoAfterEverythingRefusalsSayWhichRuleTheyRefuse(t *testing.T) {
	taker := pkit.NewModule("taker", func(w *pkit.Wiring) (module.Module, error) {
		_ = pkit.All[cartcontracts.Extension](w)
		return module.Module{Name: "taker"}, nil
	}, pkit.Needs[[]cartcontracts.Extension]())
	lastOne := pkit.NewModule("lastOne", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put(w, cartcontracts.Extension{Module: "lastOne"})
		return module.Module{Name: "lastOne"}, nil
	}, pkit.After(pkit.Everything), pkit.Contributes[cartcontracts.Extension]())

	says(t, pkit.NewApp("collect").Use(taker, lastOne).Validate(dev),
		"pkit: collect: Use: taker takes every cartcontracts.Extension, which lastOne contributes after everything: nothing may take a contribution from a module that runs after everything")
}
