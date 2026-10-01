package pkit_test

// pkit states its own rule about a contract in resolve.go: "a contract is
// either the app's one provider or a contribution, not both in one module".
// The rule is checked one module at a time, so the same two declarations in
// two modules of one app pass — and the resolver then reads the app two ways.
// A need for the one T and a Choose treat provides and contributes as one pool
// of suppliers (suppliersOf), while a module that takes every E is supplied by
// contributions alone (plan.contributors, which All and the build order read).
// So the value the provided module puts belongs to no one: Get is not how a
// many need is read and All never looks at a provider. The composition that
// does this answers with nothing — no refusal, and Explain prints the taker
// taking the contract "from no module" in the same breath as it prints the app
// building the module that provided one.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
)

func extensionTaker(name string, got *[]cartcontracts.Extension) *pkit.Module {
	return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
		*got = pkit.All[cartcontracts.Extension](w)
		return module.Module{Name: name}, nil
	}, pkit.Needs[[]cartcontracts.Extension]())
}

func extensionContributor(name string) *pkit.Module {
	return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put(w, cartcontracts.Extension{Module: name})
		return module.Module{Name: name}, nil
	}, pkit.Contributes[cartcontracts.Extension]())
}

func extensionProvider(name string) *pkit.Module {
	return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put(w, cartcontracts.Extension{Module: name})
		return module.Module{Name: name}, nil
	}, pkit.Provides[cartcontracts.Extension]())
}

// TestAContractIsProvidedOrContributedByTheAppNotByOneModule asks the rule of
// the whole app: one contract, one answer. The taker is built either way, so
// this is not about an order — it is about a value the app was given and never
// delivered, said as nothing at all.
func TestAContractIsProvidedOrContributedByTheAppNotByOneModule(t *testing.T) {
	// The honest composition: contributed by one module, taken by the one that
	// takes every Extension. The value arrives and nothing is refused.
	arrived := []cartcontracts.Extension{}
	honest := pkit.NewApp("collect").Use(extensionContributor("wishlist2"), extensionTaker("cart", &arrived))
	if err := honest.Validate(dev); err != nil {
		t.Fatalf("one contribution taken by the module that takes every Extension was refused: %v", err)
	}
	if len(arrived) != 1 || arrived[0].Module != "wishlist2" {
		t.Fatalf("the taker was built with %v, want the one contribution", arrived)
	}

	// The same app with a second module that provides the very contract the
	// other contributes: pkit forbids those two declarations in one module and
	// accepts them across two, and the provided Extension reaches no one.
	both := []cartcontracts.Extension{}
	app := pkit.NewApp("collect").Use(
		extensionProvider("stripe"), extensionContributor("wishlist2"), extensionTaker("cart", &both))
	// Either answer is honest: refuse the composition with a sentence naming the
	// contract and the two modules, or let the provided Extension reach the
	// module that takes every one. What is not honest is the third answer the
	// resolver gives today: both modules built, one value dropped, nothing said.
	err := app.Validate(dev)
	if err != nil {
		for _, want := range []string{"cartcontracts.Extension", "stripe", "wishlist2"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal omits %s: %v", want, err)
			}
		}
		return
	}
	if len(both) != 2 {
		t.Errorf("the app was given two extensions and delivered %v, and Validate refused nothing: the provided one belongs to nobody", both)
	}
	if text, explainErr := app.Explain(dev); explainErr == nil &&
		strings.Contains(text, "takes every cartcontracts.Extension from no module") {
		t.Errorf("the composition file says the taker takes the contract from no module while stripe provides it:\n%s", text)
	}
}
