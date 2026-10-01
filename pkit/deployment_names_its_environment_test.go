package pkit_test

// The environment is one fact about the deployment, so a wrong one is one
// sentence however many modules read it, and a deployment that names none is
// refused whether or not the composition declares FromDeployment. Staging is
// the environment no composition here exercised: development permits a
// simulated implementation and production refuses it, and staging refuses it
// too — a deployment that runs the real thing is not the one you simulate on.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/payment"
)

func simulatedOnly(name string) *pkit.Module {
	return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
		return module.Module{Name: name}, nil
	}, pkit.FromDeployment(pkit.Implementation{Name: "manual", Simulated: true}))
}

func TestOneDeploymentNamesItsEnvironmentOnce(t *testing.T) {
	err := pkit.NewApp("collect").Use(simulatedOnly("one"), simulatedOnly("two")).
		Validate(pkit.Deployment{Environment: "prod"})
	if err == nil {
		t.Fatal(`Validate accepted an environment it does not know`)
	}
	if n := strings.Count(err.Error(), "is not an environment"); n != 1 {
		t.Errorf("one deployment naming one bad environment said it %d times, want once: %v", n, err)
	}
	if strings.Count(err.Error(), "pkit: collect: ") != 1 {
		t.Errorf("an environment the resolver does not know answered more than the one problem it is: %v", err)
	}
	if err := pkit.NewApp("collect").Use(simulatedOnly("one")).Validate(pkit.Deployment{}); err == nil {
		t.Error(`Validate accepted a deployment that names no environment at all`)
	}
}

func TestStagingRunsTheRealImplementationAndRefusesTheSimulation(t *testing.T) {
	app := pkit.NewApp("collect").Use(payment.Module)
	says(t, app.Validate(pkit.Deployment{Environment: pkit.Staging}),
		"pkit: collect: Deployment: payment needs stripe.key in staging: set it, or the deployment cannot pick payment")
	if err := app.Validate(pkit.Deployment{Environment: pkit.Staging,
		Inputs: map[string]string{"stripe.key": "sk_test_1", "stripe.webhook.secret": "whsec_1"}}); err != nil {
		t.Fatalf("staging with the real inputs was refused: %v", err)
	}
}
