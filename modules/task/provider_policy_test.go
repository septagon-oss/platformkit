package task_test

import (
	"context"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/aserto-dev/go-authorizer/aserto/authorizer/v2"
	"github.com/aserto-dev/go-authorizer/aserto/authorizer/v2/api"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/opa"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/topaz"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/topaz/topaztest"
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type policyLeg struct {
	mu     sync.Mutex
	mode   tasktest.PolicyBehavior
	policy tenancy.Policy
	seen   []tenancy.PolicyRequest
	use    func(tasktest.PolicyBehavior)
}

func (p *policyLeg) Decide(ctx context.Context, req tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	p.mu.Lock()
	p.seen = append(p.seen, req)
	policy := p.policy
	p.mu.Unlock()
	return policy.Decide(ctx, req)
}

func (p *policyLeg) calls() []tenancy.PolicyRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]tenancy.PolicyRequest(nil), p.seen...)
}

func newPolicyLeg(t *testing.T, adapter string) *policyLeg {
	t.Helper()
	p := &policyLeg{}
	if adapter == "opa" {
		source, err := os.ReadFile("../../apps/platformkit/policy/task.rego")
		if err != nil {
			t.Fatal(err)
		}
		p.use = func(mode tasktest.PolicyBehavior) {
			rule := string(source)
			switch mode {
			case tasktest.DeniedPolicy:
				rule = `package platformkit.task
decision := {"allow": false}`
			case tasktest.UndefinedPolicy:
				rule = `package platformkit.task
decision := {"allow": true} if { false }`
			case tasktest.MissingDecision:
				rule = `package platformkit.task
decision := {"reason": "private provider diagnostic"}`
			case tasktest.WrongDecision:
				rule = `package platformkit.task
decision := {"allow": "private provider diagnostic"}`
			case tasktest.ConflictingDecision:
				rule = `package platformkit.task
decision := true`
			case tasktest.UnavailablePolicy:
				rule = `package platformkit.task
decision := {"allow": true} if { input.actor.id != "" }
decision := {"allow": false} if { input.action != "" }`
			}
			policy, err := opa.New(t.Context(), "task.rego", "platformkit.task", rule)
			if err != nil {
				t.Fatal(err)
			}
			p.mu.Lock()
			p.policy, p.mode = policy, mode
			p.mu.Unlock()
			t.Logf("embedded OPA v1.21.0; artifact %s %s", mode, policy.Revision())
		}
	} else {
		client, _ := topaztest.Client(t, func(ctx context.Context, wire *authorizer.IsRequest) (*authorizer.IsResponse, error) {
			p.mu.Lock()
			mode := p.mode
			p.mu.Unlock()
			assertTaskWire(t, wire, p.calls())
			switch mode {
			case tasktest.MissingDecision:
				return &authorizer.IsResponse{}, nil
			case tasktest.WrongDecision:
				return &authorizer.IsResponse{Decisions: []*authorizer.Decision{{Decision: "other", Is: true}}}, nil
			case tasktest.ConflictingDecision:
				return &authorizer.IsResponse{Decisions: []*authorizer.Decision{{Decision: "allowed", Is: true}, {Decision: "allowed", Is: false}}}, nil
			case tasktest.UnavailablePolicy:
				return nil, status.Error(codes.Unavailable, "private provider diagnostic")
			}
			facts := wire.ResourceContext.AsMap()
			actor := facts["actor"].(map[string]any)
			attrs := facts["resource"].(map[string]any)["attributes"].(map[string]any)
			allow := mode == tasktest.ReferencePolicy && (facts["action"] == "task:assign" ||
				facts["action"] == "task:resolve" && (attrs["assignee_id"] == "" || attrs["assignee_id"] == actor["id"]))
			return &authorizer.IsResponse{Decisions: []*authorizer.Decision{{Decision: "allowed", Is: allow}}}, nil
		})
		policy, err := topaz.New(client, topaz.Options{Path: "platformkit.task", Revision: "protocol-v1"})
		if err != nil {
			t.Fatal(err)
		}
		p.policy = policy
		p.use = func(mode tasktest.PolicyBehavior) { p.mu.Lock(); p.mode = mode; p.mu.Unlock() }
		t.Log("Topaz adapter; go-authorizer v0.24.1; gRPC/protobuf local protocol double; configured revision protocol-v1; no vendor process")
	}
	p.use(tasktest.ReferencePolicy)
	return p
}

func assertTaskWire(t *testing.T, wire *authorizer.IsRequest, calls []tenancy.PolicyRequest) {
	t.Helper()
	r := calls[len(calls)-1]
	want := map[string]any{
		"tenant":   map[string]any{"id": r.Tenant.ID.String(), "slug": r.Tenant.Slug, "operator": false},
		"actor":    map[string]any{"kind": "user", "id": r.Actor.ID},
		"action":   r.Action,
		"resource": map[string]any{"tenant_id": r.Tenant.ID.String(), "kind": "task", "id": r.Resource.ID, "attributes": r.Resource.Attributes},
	}
	if !reflect.DeepEqual(wire.ResourceContext.AsMap(), want) || wire.IdentityContext.Type != api.IdentityType_IDENTITY_TYPE_MANUAL ||
		wire.IdentityContext.Identity != r.Actor.ID || wire.PolicyContext.Path != "platformkit.task" || !reflect.DeepEqual(wire.PolicyContext.Decisions, []string{"allowed"}) {
		t.Errorf("wire lost trusted facts: %v; want %#v", wire, want)
	}
}
