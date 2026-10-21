package pkit_test

// A refused module's partial outputs are discarded: the module that runs after
// everything reads the manifests of the modules that built cleanly, and not the
// manifest of one that put something and then refused.

import (
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/user"
)

func TestARefusedModulesOutputsAreNotSeenAfterIt(t *testing.T) {
	var saw []string
	last := pkit.NewModule("last", func(w *pkit.Wiring) (module.Module, error) {
		for _, m := range pkit.Composition(w) {
			saw = append(saw, m.Name)
		}
		return module.Module{Name: "last"}, nil
	}, pkit.After(pkit.Everything))
	if err := pkit.NewApp("collect").Use(user.Module, halfModule(), last).Validate(dev); err == nil {
		t.Fatal("a composition with a refused module resolved")
	}
	if !slices.Contains(saw, "user") || slices.Contains(saw, "half") {
		t.Errorf("the module that runs after everything saw %q, want user and not the refused half", saw)
	}
}
