package tenancy_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/internal/exampletest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func ExamplePolicy() {
	calls := 0
	policy := decisionFunc(func(_ context.Context, r tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
		calls++
		decision := tenancy.PolicyDecision{Reason: "write denied", Revision: "example-v1"}
		if r.Action == "task:read" {
			decision.Allowed, decision.Reason = true, "read allowed"
		}
		return decision, nil
	})
	tenant := tenancy.Tenant{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111")}
	request := tenancy.PolicyRequest{
		Tenant: tenant, Actor: tenancy.PolicyActor{Kind: tenancy.PolicyUser, ID: "reader"},
		Action: "task:read", Resource: tenancy.PolicyResource{TenantID: tenant.ID, Kind: "task", ID: "sample"},
	}
	allowed, err := tenancy.RequirePolicy(context.Background(), policy, request)
	if err != nil {
		panic(err)
	}
	fmt.Println(allowed.Allowed, allowed.Revision)
	request.Action = "task:write"
	denied, err := tenancy.RequirePolicy(context.Background(), policy, request)
	if !errors.Is(err, tenancy.ErrPolicyDenied) {
		panic(fmt.Errorf("expected policy denial, got %v", err))
	}
	fmt.Println(denied.Reason, errors.Is(err, tenancy.ErrPolicyDenied))
	fmt.Println("decisions:", calls)
	// Output:
	// true example-v1
	// write denied true
	// decisions: 2
}

func TestDocumentedExamplesExecute(t *testing.T) {
	exampletest.Execute(t, "ExamplePolicy")
}
