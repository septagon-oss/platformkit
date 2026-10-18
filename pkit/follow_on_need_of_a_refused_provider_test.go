package pkit_test

// A module that dies under a refusal takes the values it would have provided
// with it, and the module that needed one of those values is not built either.
// The follow-on owes no sentence of its own: the composition has already said
// why the provider is gone, and a second sentence about it would name a module
// to add that Use already holds, or a choice the app never made — a value a
// client could act on, wrong in the direction of adding a module nobody needs.
// One refusal, one sentence; the follow-on is silence, not an echo.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
	usercontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/user/contracts"
)

func TestAModuleWhoseProviderWasRefusedAddsNoSecondSentence(t *testing.T) {
	quiet := func(name string, decls ...pkit.Declaration) *pkit.Module {
		return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
			return module.Module{Name: name}, nil
		}, decls...)
	}
	provider := quiet("depot", pkit.Provides[cartcontracts.Service](), pkit.Needs[usercontracts.Service]())
	consumer := quiet("taker", pkit.Needs[cartcontracts.Service]())

	err := pkit.NewApp("collect").Use(provider, consumer).Validate(dev)
	says(t, err, "pkit: collect: Use: depot needs usercontracts.Service: add user.Module to collect")
	for _, untrue := range []string{"taker", "chose"} {
		if strings.Contains(err.Error(), untrue) {
			t.Errorf("the follow-on need speaks, and says %q where the composition had already said the one sentence that is true: %v", untrue, err)
		}
	}
	if lines := strings.Count(err.Error(), "\n") + 1; lines > 1 {
		t.Errorf("one refused provider answered %d sentences, want the one: %v", lines, err)
	}
}
