package pkit_test

// An Optional contract the app composes no provider for reads as the zero
// value, and the composition resolves: "not composed" is an answer, not a
// refusal.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/user"
)

func TestAnOptionalReadOfAnUncomposedContractIsZero(t *testing.T) {
	var saw heldPort = holder{}
	uses := pkit.NewModule("uses", func(w *pkit.Wiring) (module.Module, error) {
		saw = pkit.Get[heldPort](w)
		return module.Module{Name: "uses"}, nil
	}, pkit.Optional[heldPort]())
	if err := pkit.NewApp("collect").Use(user.Module, uses).Validate(dev); err != nil {
		t.Fatalf("an optional contract nobody composes refused the composition: %v", err)
	}
	if saw != nil {
		t.Errorf("the optional read answered %v, want the zero value", saw)
	}
}
