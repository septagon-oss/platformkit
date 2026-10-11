package pkit_test

// pkit's own rule about Provides and Contributes belongs to the contract, not
// to the module that says it: one contract has one answer in the app. Read two
// ways the pair drops a value — Choose and Needs[T] count both declarations as
// suppliers of the contract, while a module that takes every E reads the
// contributions alone — so the module that Provides it is built, Puts, and
// belongs to nobody. Refusing is the answer this package chose; Choose is not
// the fix, because withdrawing one half of the pair leaves the same dropped
// value with a choice in front of it.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
)

func extensionExtender(name string) *pkit.Module {
	return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put(w, cartcontracts.Extension{Module: name})
		return module.Module{Name: name}, nil
	}, pkit.Provides[cartcontracts.Extension]())
}

func extensionAdder(name string) *pkit.Module {
	return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put(w, cartcontracts.Extension{Module: name})
		return module.Module{Name: name}, nil
	}, pkit.Contributes[cartcontracts.Extension]())
}

func extensionCollector(name string) *pkit.Module {
	return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
		return module.Module{Name: name}, nil
	}, pkit.Needs[[]cartcontracts.Extension]())
}

func TestTheAppProvidesOrContributesOneContractNotBoth(t *testing.T) {
	says(t, pkit.NewApp("collect").Use(extensionExtender("stripe"), extensionAdder("wishlist"), extensionCollector("cart")).Validate(dev),
		"pkit: collect: Use: cartcontracts.Extension is provided by stripe.Module and contributed by wishlist.Module"+
			": a contract is either the app's one provider or a contribution, not both in one app"+
			" — make one of them the other; Choose cannot settle it")
	// A single need for the same pair is refused by that sentence alone: the
	// follow-on that would name Choose is the fix that cannot settle it.
	says(t, pkit.NewApp("collect").Use(extensionExtender("stripe"), extensionAdder("wishlist"),
		pkit.NewModule("pay", func(w *pkit.Wiring) (module.Module, error) { return module.Module{Name: "pay"}, nil },
			pkit.Needs[cartcontracts.Extension]())).Validate(dev),
		"cartcontracts.Extension is provided by stripe.Module and contributed by wishlist.Module")
	if err := pkit.NewApp("collect").Use(extensionExtender("stripe"), extensionAdder("wishlist"), extensionCollector("cart")).Validate(dev); err != nil && strings.Contains(err.Error(), "Choose one in collect") {
		t.Errorf("the refusal names Choose, which cannot settle a contract one module provides and another contributes: %v", err)
	}
	// Several contributors do not split the sentence, and the order of Use does
	// not change it.
	says(t, pkit.NewApp("collect").Use(extensionCollector("cart"), extensionExtender("stripe"),
		extensionAdder("wishlist"), extensionAdder("shipping")).Validate(dev),
		"cartcontracts.Extension is provided by stripe.Module and contributed by wishlist.Module and shipping.Module")
	// Choosing one of the pair is not the fix: the app refuses for the pair it
	// was given, not for whichever half survived.
	prov := extensionExtender("stripe")
	says(t, pkit.NewApp("collect").Use(prov, extensionAdder("wishlist"), extensionCollector("cart")).
		Choose(prov).Validate(dev),
		"cartcontracts.Extension is provided by stripe.Module and contributed by wishlist.Module")
}
