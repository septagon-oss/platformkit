package pkit_test

// An environment belongs to the deployment, not to the modules: 0074 rule 3 gives
// FromDeployment two answers, one for development and one for production, so a
// deployment that names no environment the resolver knows has nothing to pick.
// The resolver says so — but only on the way to picking an implementation, so a
// composition with no FromDeployment module resolves, builds and Explains under
// a name that is not an environment. "prod" for "production" is one dropped
// letter, and Development is the only spelling that permits a simulated
// implementation.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/payment"
)

func TestAnEnvironmentIsCheckedWhereverTheDeploymentNamesIt(t *testing.T) {
	// Where the check exists, this is what it says.
	says(t, pkit.NewApp("collect").Use(payment.Module).Validate(pkit.Deployment{Environment: "prod"}),
		`pkit: collect: Deployment: "prod" is not an environment: name development, staging or production`)

	// A composition that declares no FromDeployment module must refuse the same
	// deployment for the same reason: the environment is the deployment's.
	lonely := pkit.NewModule("lonely", func(w *pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "lonely"}, nil
	})
	app := pkit.NewApp("collect").Use(lonely)
	if err := app.Validate(pkit.Deployment{Environment: "prod"}); err == nil {
		t.Error(`Validate accepted an environment it does not know`)
	}
	if _, err := app.Explain(pkit.Deployment{Environment: "prod"}); err == nil {
		t.Error(`Explain printed an environment it was never told`)
	}
}
