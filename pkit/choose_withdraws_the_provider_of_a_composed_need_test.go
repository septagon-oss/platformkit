package pkit_test

// A module may provide more than one contract, and Choose withdraws the whole
// module it did not pick. So does every hard need the app composed against it:
// `collectibles needs cartcontracts.Service` when the only module that provided
// the cart service is the payment module the app just chose away. That need is
// the one failure 0074 rule 3 names first — "collectibles needs
// cartcontracts.Service: add cart.Module to collect" — and the resolver is where
// it is answered. The suppression that keeps a module whose supplier *failed*
// from repeating the sentence the composition already said must not swallow the
// case where the supplier was withdrawn by a choice, because a choice is not a
// failure and no sentence was said about it. Left unsaid, the module is dropped:
// Validate answers nil, Explain prints a composition file that never names it,
// and the app that would serve requests is smaller than the one Use describes.
//
// The honest answer is either of the two this test accepts — refuse, naming the
// module and the contract, or build the module and wire it — and never the third
// one, which is to accept a composition that silently omits a module the client
// composed.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/collectibles"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/payment"
	paymentcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/payment/contracts"
)

type manualCharge struct{}

func (manualCharge) Charge(minor int64) string { return fmt.Sprintf("manual:%d", minor) }

type manualTotal struct{}

func (manualTotal) Total() int64 { return 0 }

// manualPay is one module that is the app's one answer to two contracts: the
// payment provider the deployment picks between two, and the cart service
// collectibles cannot be built without.
func manualPay() *pkit.Module {
	return pkit.NewModule("manual", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put(w, paymentcontracts.Provider(manualCharge{}))
		pkit.Put(w, cartcontracts.Service(manualTotal{}))
		return module.Module{Name: "manual"}, nil
	}, pkit.Provides[paymentcontracts.Provider](), pkit.Provides[cartcontracts.Service]())
}

func TestAChoiceThatWithdrawsTheOnlyProviderOfAComposedNeedIsRefused(t *testing.T) {
	// Choosing the module that provides both is honest: all three are composed,
	// payment loses the choice and nothing needed it. This half passes today and
	// must keep passing — the fix is a sentence, not a ban on Choose.
	keptModule := manualPay()
	kept := pkit.NewApp("collect").Use(keptModule, payment.Module, collectibles.Module).Choose(keptModule)
	if err := kept.Validate(dev); err != nil {
		t.Fatalf("the app that chooses the module providing both contracts was refused: %v", err)
	}
	keptText, err := kept.Explain(dev)
	if err != nil {
		t.Fatalf("Explain of the honest choice failed: %v", err)
	}
	if !strings.Contains(keptText, "collectibles.Module") {
		t.Errorf("the composition file of the app that kept both providers does not name collectibles:\n%s", keptText)
	}

	// Choosing the other payment module withdraws manual whole, and with it the
	// only cartcontracts.Service the app composed. collectibles still names that
	// need in Use.
	given := []string{"manual", "payment", "collectibles"}
	givenModule := manualPay()
	withdrawn := pkit.NewApp("collect").Use(givenModule, payment.Module, collectibles.Module).Choose(payment.Module)
	err = withdrawn.Validate(dev)
	if err == nil {
		text, explainErr := withdrawn.Explain(dev)
		if explainErr != nil {
			t.Fatalf("Validate accepted the composition and Explain refused it: %v", explainErr)
		}
		for _, named := range given {
			if !strings.Contains(text, named+".Module") {
				t.Errorf("Validate accepted a composition and Explain prints one without %s, which Use named:\n%s", named, text)
			}
		}
		return
	}
	if !strings.Contains(err.Error(), "collectibles") || !strings.Contains(err.Error(), "cartcontracts.Service") {
		t.Errorf("the refusal does not name the module left without its need and the contract it needs: %v", err)
	}
}
