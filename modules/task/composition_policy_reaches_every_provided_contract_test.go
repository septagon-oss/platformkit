package task_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

type allowAll struct{}

func (allowAll) Decide(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	return tenancy.PolicyDecision{Allowed: true, Revision: "v1"}, nil
}

// With a policy composed, the four provided contracts are still one value, so a
// consumer's Creator is never the policy-less desk beside a policed Service.
func TestComposedPolicyReachesEveryProvidedTaskContract(t *testing.T) {
	policy := pkit.NewModule("policy", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[tenancy.Policy](w, allowAll{})
		return module.Module{Name: "policy"}, nil
	}, pkit.Provides[tenancy.Policy]())
	plan, err := pkit.NewApp("desk").Use(deskInputs(), policy, task.Module).
		Plan(pkit.Deployment{Environment: pkit.Development})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	svc := mustValue[taskcontracts.Service](t, plan)
	for name, got := range map[string]any{
		"Creator":      mustValue[taskcontracts.Creator](t, plan),
		"Reader":       mustValue[taskcontracts.Reader](t, plan),
		"LockedReader": mustValue[taskcontracts.LockedReader](t, plan),
	} {
		if got != any(svc) {
			t.Errorf("%s is %v, not the policed Service %v", name, got, svc)
		}
	}
}
