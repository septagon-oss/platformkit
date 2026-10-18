package pkit_test

// 0074 rule 3 puts two rows in the same table: a module that "runs after every
// other module" and one whose contract "many contribute to, and the one is
// built after all of them". A module cannot be both: whoever takes the
// contribution would be built after the module that runs after everything.
// The resolver already refuses that shape for a need — "nothing may need a
// module that runs after everything" — and the contribution shape has to be
// refused the same way. What happens today is that the taker is left out of the
// order, so it is never built, Validate answers nothing, and Explain prints a
// composition that names a module it did not build.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
)

func TestNoModuleIsLeftOutOfTheCompositionItCannotOrder(t *testing.T) {
	var tookBuilt bool
	taker := pkit.NewModule("taker", func(w *pkit.Wiring) (module.Module, error) {
		tookBuilt = true
		_ = pkit.All[cartcontracts.Extension](w)
		return module.Module{Name: "taker"}, nil
	}, pkit.Needs[[]cartcontracts.Extension]())

	lastOne := pkit.NewModule("lastOne", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put(w, cartcontracts.Extension{Module: "lastOne"})
		return module.Module{Name: "lastOne"}, nil
	}, pkit.After(pkit.Everything), pkit.Contributes[cartcontracts.Extension]())

	app := pkit.NewApp("collect").Use(taker, lastOne)

	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("Validate panicked: %v", r)
			}
		}()
		return app.Validate(dev)
	}()
	if err != nil {
		if !strings.Contains(err.Error(), "taker") || !strings.Contains(err.Error(), "lastOne") {
			t.Errorf("the refusal names neither the taker nor the module that runs after everything: %v", err)
		}
		return
	}
	if !tookBuilt {
		t.Fatal("collect accepted a composition it could not order: taker.Module is in Use, is not refused, and was never built")
	}

	txt, explainErr := func() (txt string, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("Explain panicked: %v", r)
			}
		}()
		s, e := app.Explain(dev)
		if e != nil {
			return "", e
		}
		return s, nil
	}()
	if explainErr != nil && strings.HasPrefix(explainErr.Error(), "Explain panicked:") {
		t.Fatalf("%v", explainErr)
	}
	if !strings.Contains(txt, "taker.Module takes every cartcontracts.Extension from lastOne.Module") {
		t.Errorf("the composition file does not say who contributes to the module that takes every one:\n%s", txt)
	}
}
