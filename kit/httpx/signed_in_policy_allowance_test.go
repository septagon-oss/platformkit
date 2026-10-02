package httpx_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type objectPolicyAllowance struct{}

func (objectPolicyAllowance) Decide(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	return tenancy.PolicyDecision{Allowed: true, Revision: "policy-v1"}, nil
}

// A signed-in command whose object policy admits the row is not a refusal: the
// observer behind the guard records nothing and hands the audit composition nothing.
func TestASignedInCommandThePolicyAdmitsRecordsNoRefusal(t *testing.T) {
	api, router, fixture := setup(t)
	fixture.signedIn()
	fixture.allow = true
	runs := 0
	recorded := true
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "signed-in-allowed-command", Method: http.MethodPost, Path: "/signed-in-allowed-command",
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		runs++
		tenant, _ := tenancy.FromContext(ctx)
		actor, _ := tenancy.PrincipalFrom(ctx)
		if _, err := tenancy.RequirePolicy(ctx, objectPolicyAllowance{}, tenancy.PolicyRequest{
			Tenant: tenant, Actor: tenancy.PolicyActor{Kind: tenancy.PolicyUser, ID: actor.UserID.String()},
			Action: "task:resolve", Resource: tenancy.PolicyResource{TenantID: tenant.ID, Kind: "task", ID: "row-of-their-own"},
		}); err != nil {
			t.Fatalf("the object policy answered %v, want an allowance", err)
		}
		_, recorded = httpx.Refused(ctx)
		return nil, nil
	})

	fixture.denials = nil
	if got := request(t, router, http.MethodPost, at(api, "/signed-in-allowed-command"), host); got.Code >= 300 {
		t.Fatalf("POST /signed-in-allowed-command = %d, want success", got.Code)
	}
	if runs != 1 {
		t.Fatalf("the command ran %d times, want once", runs)
	}
	if recorded {
		t.Error("an admitted row was recorded as this request's refusal")
	}
	if len(fixture.denials) != 0 {
		t.Errorf("an admitted row handed the audit composition %+v, want nothing", fixture.denials)
	}
}
