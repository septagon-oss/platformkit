package pkit_test

// FromDeployment() with nothing inside it is a module that names no
// implementation for the deployment to pick. The refusal is right; the sentence
// it used to print pointed at a simulation the module never declared, so the
// client went looking for an Implementation that is not in the file.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestFromDeploymentWithNoImplementationNamesNoSimulation(t *testing.T) {
	none := pkit.NewModule("none", func(w *pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "none"}, nil
	}, pkit.FromDeployment())
	err := pkit.NewApp("collect").Use(none).Validate(dev)
	says(t, err, "pkit: collect: Deployment: none declares FromDeployment with no implementation to pick: name one, or do not declare it")
	if strings.Contains(err.Error(), "only simulates") {
		t.Errorf("the refusal blames a simulation this module never declared: %v", err)
	}
}
