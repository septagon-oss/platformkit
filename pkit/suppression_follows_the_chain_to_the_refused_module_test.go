package pkit_test

// Suppression carries down the build order: a module that waits on a suppressed
// module is suppressed too, every suppressed module is attributed to the module
// that refused, and the list is in build order. None of them earns a sentence.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestSuppressionFollowsTheChainToTheRefusedModule(t *testing.T) {
	quiet := func(name string, decls ...pkit.Declaration) *pkit.Module {
		return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
			return module.Module{Name: name}, nil
		}, decls...)
	}
	mid := pkit.NewModule("mid", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[midPort](w, holder{})
		return module.Module{Name: "mid"}, nil
	}, pkit.Needs[heldPort](), pkit.Provides[midPort]())
	end := quiet("end", pkit.Needs[midPort]())
	side := quiet("side", pkit.Optional[heldPort]())

	err := pkit.NewApp("collect").Use(halfModule(), mid, end, side).Validate(dev)
	ce := compositionError(t, err)
	if len(ce.Problems) != 1 || ce.Problems[0].Module != "half" {
		t.Errorf("the problems are %+v, want the one about half", ce.Problems)
	}
	want := []pkit.DescribedSuppression{{Module: "mid", By: "half"}, {Module: "side", By: "half"}, {Module: "end", By: "half"}}
	if !reflect.DeepEqual(ce.Suppressed, want) {
		t.Errorf("the suppressed modules are %+v, want %+v in build order", ce.Suppressed, want)
	}
	for _, name := range []string{"mid", "end", "side"} {
		if strings.Contains(err.Error(), name) {
			t.Errorf("a suppressed module, %s, is named in the refusal:\n%v", name, err)
		}
	}
}
