package pkit_test

import (
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

type finiteDeclarations []string

func TestNamedSliceIsOneRequiredContract(t *testing.T) {
	var arrived finiteDeclarations
	provider := pkit.NewModule("declarations", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put(w, finiteDeclarations{"one", "two"})
		return module.Module{Name: "declarations"}, nil
	}, pkit.Provides[finiteDeclarations]())
	consumer := pkit.NewModule("renderer", func(w *pkit.Wiring) (module.Module, error) {
		arrived = pkit.Get[finiteDeclarations](w)
		return module.Module{Name: "renderer"}, nil
	}, pkit.Needs[finiteDeclarations]())
	if err := pkit.NewApp("pictures").Use(consumer).Validate(dev); err == nil {
		t.Fatal("missing finite declaration provider accepted")
	}
	if err := pkit.NewApp("pictures").Use(provider, consumer).Validate(dev); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(arrived, finiteDeclarations{"one", "two"}) {
		t.Fatalf("named contract arrived as %v", arrived)
	}
}
