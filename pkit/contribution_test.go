package pkit_test

// A module that declares Contributes[T] and does not put one T is a mistake
// the composition has to answer with a sentence, the same way a module that
// declares Provides[T] and does not put it is answered today. Get of a need
// whose supplier put nothing indexes an empty slice, so the mistake currently
// crashes the process instead of naming the module, the contract and the fix.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/homepage"
	homepagecontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/homepage/contracts"
)

// silentContributor declares it contributes a landing page and puts one only
// when asked to — the shape of a product module that contributes its landing
// only when the feature is configured.
func silentContributor(putOne bool) *pkit.Module {
	return pkit.NewModule("banner", func(w *pkit.Wiring) (module.Module, error) {
		if putOne {
			pkit.Put(w, homepagecontracts.Landing{Path: "/banner"})
		}
		return module.Module{Name: "banner"}, nil
	}, pkit.Contributes[homepagecontracts.Landing]())
}

func TestAContributionNobodyPutsIsRefusedWithASentence(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Validate panicked instead of refusing: %v", r)
		}
	}()
	app := pkit.NewApp("collect").Use(homepage.Module, silentContributor(false))
	err := app.Validate(dev)
	if err == nil {
		t.Fatal("a module that contributes nothing was accepted")
	}
	if !strings.Contains(err.Error(), "banner") || !strings.Contains(err.Error(), "Landing") {
		t.Errorf("the refusal names neither the module nor the contract: %v", err)
	}

	// The same composition with the contribution actually put is honest.
	if err := pkit.NewApp("collect").Use(homepage.Module, silentContributor(true)).Validate(dev); err != nil {
		t.Errorf("the honest composition was refused: %v", err)
	}
}
