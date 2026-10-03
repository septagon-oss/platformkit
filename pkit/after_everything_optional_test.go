package pkit_test

// "Nothing may need a module that runs after everything" has to hold for the
// optional spelling too. An Optional need is still a build edge — the module
// that uses the value is built after the one that supplies it — so the same
// order that cannot hold a required need of the last module cannot hold this
// one either. Left unrefused it is worse than the required case: the module
// that asks for it is dropped from the order in silence, so the composition
// both loses a module and prints a composition file that says so without
// saying it lost anything.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/admin"
	admincontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/admin/contracts"
	paymentcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/payment/contracts"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/paypal"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/stripe"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/user"
)

func TestNothingMayUseAModuleThatRunsAfterEverything(t *testing.T) {
	app := pkit.NewApp("collect").Use(user.Module, admin.Module, optionalConsole())

	err := app.Validate(dev)
	says(t, err, "pkit: collect: Use: fan uses admincontracts.Console, which admin provides after everything: nothing may need a module that runs after everything")
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("one impossible edge answered %d sentences, want the one: %v", strings.Count(err.Error(), "\n")+1, err)
	}

	text, err := app.Explain(dev)
	if err == nil {
		t.Errorf("Explain printed a composition that dropped a module it was given:\n%s", text)
	}
}

func optionalConsole() *pkit.Module {
	return pkit.NewModule("fan", func(w *pkit.Wiring) (module.Module, error) {
		_ = pkit.Get[admincontracts.Console](w)
		return module.Module{Name: "fan"}, nil
	}, pkit.Optional[admincontracts.Console]())
}

// Choose can withdraw the supplier of the last module's need, which is the
// half of that refusal which was named and left unbuilt. The last module is now
// resolved like every other one, so the withdrawal reaches it: an ambiguous
// optional need is the ambiguity sentence, and the Choose the sentence asks
// for settles it onto the module that survives.
func TestChooseReachesTheNeedOfTheModuleThatRunsAfterEverything(t *testing.T) {
	lastWantsProvider := func() *pkit.Module {
		return pkit.NewModule("adm", func(w *pkit.Wiring) (module.Module, error) {
			_ = pkit.Get[paymentcontracts.Provider](w)
			return module.Module{Name: "adm"}, nil
		}, pkit.After(pkit.Everything), pkit.Optional[paymentcontracts.Provider]())
	}

	both := pkit.NewApp("collect").Use(user.Module, stripe.Module, paypal.Module, lastWantsProvider())
	says(t, both.Validate(dev),
		"pkit: collect: Choose: paymentcontracts.Provider has stripe.Module and paypal.Module: Choose one in collect")

	picked := pkit.NewApp("collect").Use(user.Module, stripe.Module, paypal.Module, lastWantsProvider()).
		Choose(stripe.Module)
	if err := picked.Validate(dev); err != nil {
		t.Fatalf("doing what the sentence said still failed: %v", err)
	}
	text, err := picked.Explain(dev)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"pkit: adm.Module uses paymentcontracts.Provider from stripe.Module.",
		"pkit: collect chose stripe.Module over paypal.Module for paymentcontracts.Provider.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the composition file does not read %q:\n%s", want, text)
		}
	}
}
