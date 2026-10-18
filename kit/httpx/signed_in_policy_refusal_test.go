package httpx_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type objectPolicyDenial struct{}

func (objectPolicyDenial) Decide(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	return tenancy.PolicyDecision{Reason: "the actor does not own this row", Revision: "policy-v1"}, nil
}

func TestASignedInCommandRecordsItsObjectPolicyRefusal(t *testing.T) {
	api, router, fixture := setup(t)
	fixture.signedIn()
	fixture.allow = true
	runs := 0
	var refusal httpx.Refusal
	var recorded bool
	command := func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		runs++
		tenant, hasTenant := tenancy.FromContext(ctx)
		actor, hasActor := tenancy.PrincipalFrom(ctx)
		if !hasTenant || !hasActor {
			t.Fatal("the signed-in command did not receive its tenant and actor")
		}
		_, err := tenancy.RequirePolicy(ctx, objectPolicyDenial{}, tenancy.PolicyRequest{
			Tenant: tenant, Actor: tenancy.PolicyActor{Kind: tenancy.PolicyUser, ID: actor.UserID.String()},
			Action: "task:resolve", Resource: tenancy.PolicyResource{
				TenantID: tenant.ID, Kind: "task", ID: "row-owned-by-somebody-else",
			},
		})
		if !errors.Is(err, tenancy.ErrPolicyDenied) {
			t.Fatalf("the object policy answered %v, want ErrPolicyDenied", err)
		}
		refusal, recorded = httpx.Refused(ctx)
		return nil, problem.New(http.StatusForbidden, "POLICY_DENIED: this action is not allowed")
	}
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "permission-command", Method: http.MethodPost, Path: "/permission-command",
	}, httpx.Permission("task:resolve"), command)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "signed-in-command", Method: http.MethodPost, Path: "/signed-in-command",
	}, httpx.SignedIn(), command)

	for _, path := range []string{"/permission-command", "/signed-in-command"} {
		fixture.denials = nil
		if got := request(t, router, http.MethodPost, at(api, path), host); got.Code != http.StatusForbidden {
			t.Fatalf("POST %s = %d, want the object's 403 refusal", path, got.Code)
		}
		if runs == 0 {
			t.Fatalf("POST %s did not reach the command", path)
		}
		if !recorded || refusal.Code != httpx.CodePolicyDenied || refusal.Reason != "the actor does not own this row" || refusal.Revision != "policy-v1" {
			t.Errorf("POST %s recorded refusal %+v (present %v), want the policy's reason and revision", path, refusal, recorded)
		}
		if len(fixture.denials) != 1 || fixture.denials[0].Code != httpx.CodePolicyDenied {
			t.Errorf("POST %s handed the audit composition %+v, want one POLICY_DENIED record", path, fixture.denials)
		}
	}
	if runs != 2 {
		t.Errorf("the two commands ran %d times, want once each", runs)
	}
}
