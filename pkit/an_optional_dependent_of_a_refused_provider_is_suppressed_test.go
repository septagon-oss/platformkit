package pkit_test

// A module that optionally uses a contract whose composed provider refused is
// suppressed, not built over a zero: the provider was composed, so its absence
// is this plan's failure and not the "not composed" an Optional read answers.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestAnOptionalDependentOfARefusedProviderIsSuppressed(t *testing.T) {
	ran := false
	userOf := pkit.NewModule("user_of", func(w *pkit.Wiring) (module.Module, error) {
		ran = true
		return module.Module{Name: "user_of"}, nil
	}, pkit.Optional[heldPort]())
	err := pkit.NewApp("collect").Use(halfModule(), userOf).Validate(dev)
	ce := compositionError(t, err)
	if ran {
		t.Error("the optional dependent of a refused provider was built")
	}
	if len(ce.Problems) != 1 || ce.Problems[0].Module != "half" {
		t.Errorf("the problems are %+v, want the one about half", ce.Problems)
	}
	if !reflect.DeepEqual(ce.Suppressed, []pkit.DescribedSuppression{{Module: "user_of", By: "half"}}) {
		t.Errorf("the suppressed modules are %+v, want user_of, by half", ce.Suppressed)
	}
	if strings.Contains(err.Error(), "\n") || strings.Contains(err.Error(), "user_of") {
		t.Errorf("want one sentence and no word about the suppressed module:\n%v", err)
	}
}
