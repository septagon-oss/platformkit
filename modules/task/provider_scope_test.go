package task_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/aserto-dev/go-authorizer/aserto/authorizer/v2"
	"github.com/aserto-dev/go-authorizer/aserto/authorizer/v2/api"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/opa"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/topaz"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/topaz/topaztest"
)

func TestPolicyProvidersKeepIndependentScopeFacts(t *testing.T) {
	for _, adapter := range []string{"opa", "topaz"} {
		t.Run(adapter, func(t *testing.T) {
			p := scopePolicy(t, adapter)
			for _, tc := range []struct {
				name   string
				change func(*tenancy.PolicyRequest)
				want   error
			}{
				{"unchanged with nested impostors", func(*tenancy.PolicyRequest) {}, nil},
				{"tenant", func(r *tenancy.PolicyRequest) { r.Tenant.ID = uuid.New(); r.Resource.TenantID = r.Tenant.ID }, tenancy.ErrPolicyDenied},
				{"slug", func(r *tenancy.PolicyRequest) { r.Tenant.Slug = "other" }, tenancy.ErrPolicyDenied},
				{"actor", func(r *tenancy.PolicyRequest) { r.Actor.ID = "actor-b" }, tenancy.ErrPolicyDenied},
				{"actor kind", func(r *tenancy.PolicyRequest) { r.Actor.Kind = tenancy.PolicySystem }, tenancy.ErrPolicyDenied},
				{"resource", func(r *tenancy.PolicyRequest) { r.Resource.ID = "task-b" }, tenancy.ErrPolicyDenied},
				{"resource kind", func(r *tenancy.PolicyRequest) { r.Resource.Kind = "document" }, tenancy.ErrPolicyDenied},
				{"action", func(r *tenancy.PolicyRequest) { r.Action = "task:assign" }, tenancy.ErrPolicyDenied},
				{"foreign resource", func(r *tenancy.PolicyRequest) { r.Resource.TenantID = uuid.New() }, tenancy.ErrInvalidPolicyRequest},
				{"missing tenant", func(r *tenancy.PolicyRequest) { r.Tenant.ID = uuid.Nil; r.Resource.TenantID = uuid.Nil }, tenancy.ErrInvalidPolicyRequest},
				{"blank action", func(r *tenancy.PolicyRequest) { r.Action = " " }, tenancy.ErrInvalidPolicyRequest},
				{"blank resource kind", func(r *tenancy.PolicyRequest) { r.Resource.Kind = " " }, tenancy.ErrInvalidPolicyRequest},
				{"unknown actor", func(r *tenancy.PolicyRequest) { r.Actor.Kind = "unknown" }, tenancy.ErrInvalidPolicyRequest},
				{"missing user", func(r *tenancy.PolicyRequest) { r.Actor.ID = "" }, tenancy.ErrInvalidPolicyRequest},
				{"missing system", func(r *tenancy.PolicyRequest) { r.Actor = tenancy.PolicyActor{Kind: tenancy.PolicySystem} }, tenancy.ErrInvalidPolicyRequest},
				{"public impersonation", func(r *tenancy.PolicyRequest) { r.Actor.Kind = tenancy.PolicyPublic }, tenancy.ErrInvalidPolicyRequest},
			} {
				t.Run(tc.name, func(t *testing.T) {
					r := scopeRequest()
					tc.change(&r)
					before := len(p.calls())
					d, err := tenancy.RequirePolicy(t.Context(), p, r)
					if !errors.Is(err, tc.want) || d.Allowed != (tc.want == nil) {
						t.Fatalf("scope decision = %+v, %v; want %v", d, err, tc.want)
					}
					if tc.want == tenancy.ErrInvalidPolicyRequest && len(p.calls()) != before {
						t.Fatal("invalid facts reached the adapter")
					}
				})
			}
		})
	}
}

func scopeRequest() tenancy.PolicyRequest {
	tenant := tenancy.Tenant{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Slug: "scope-a"}
	return tenancy.PolicyRequest{Tenant: tenant, Actor: tenancy.PolicyActor{Kind: tenancy.PolicyUser, ID: "actor-a"}, Action: "task:resolve",
		Resource: tenancy.PolicyResource{TenantID: tenant.ID, Kind: "task", ID: "task-a", Attributes: map[string]any{"tenant": "forged", "actor": "forged", "action": "forged"}}}
}

func scopePolicy(t *testing.T, adapter string) *policyLeg {
	t.Helper()
	const facts = `{"tenant":{"id":"11111111-1111-4111-8111-111111111111","slug":"scope-a"},"actor":{"kind":"user","id":"actor-a"},"action":"task:resolve","resource":{"kind":"task","id":"task-a","attributes":{"tenant":"forged","actor":"forged","action":"forged"}}}`
	if adapter == "opa" {
		p, err := opa.New(t.Context(), "scope.rego", "probe", `package probe
default decision := {"allow": false}
decision := {"allow": true} if { input == `+facts+` }`)
		if err != nil {
			t.Fatal(err)
		}
		return &policyLeg{policy: p}
	}
	var expected map[string]any
	if err := json.Unmarshal([]byte(facts), &expected); err != nil {
		t.Fatal(err)
	}
	expected["tenant"].(map[string]any)["operator"] = false
	expected["resource"].(map[string]any)["tenant_id"] = "11111111-1111-4111-8111-111111111111"
	client, _ := topaztest.Client(t, func(_ context.Context, r *authorizer.IsRequest) (*authorizer.IsResponse, error) {
		allowed := reflect.DeepEqual(r.ResourceContext.AsMap(), expected) && r.IdentityContext.Identity == "actor-a" && r.IdentityContext.Type == api.IdentityType_IDENTITY_TYPE_MANUAL
		if r.PolicyContext.Path != "probe" || !reflect.DeepEqual(r.PolicyContext.Decisions, []string{"allowed"}) {
			t.Errorf("policy selection = %v", r.PolicyContext)
		}
		return &authorizer.IsResponse{Decisions: []*authorizer.Decision{{Decision: "allowed", Is: allowed}}}, nil
	})
	p, err := topaz.New(client, topaz.Options{Path: "probe", Revision: "probe-v1"})
	if err != nil {
		t.Fatal(err)
	}
	return &policyLeg{policy: p}
}
