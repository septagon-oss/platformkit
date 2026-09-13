// Package topaz adapts the official authorizer client to tenancy.Policy.
// The constructing application owns the connection and its lifecycle.
package topaz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aserto-dev/go-authorizer/aserto/authorizer/v2"
	"github.com/aserto-dev/go-authorizer/aserto/authorizer/v2/api"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Options selects the decision in the policy artifact deployed by the
// application. Revision records that deployment's configured revision; Topaz's
// Is response does not return a policy revision or freshness guarantee.
type Options struct {
	Path     string
	Decision string
	Revision string
	// Timeout bounds each evaluation. Zero uses two seconds; a caller's
	// earlier deadline still takes precedence.
	Timeout time.Duration
}

type topazPolicy struct {
	client  authorizer.AuthorizerClient
	options Options
}

// New adapts the official Topaz client to application policy checks.
// The application owns the client's connection, transport credentials and close.
// Decision defaults to "allowed"; Path and Revision must identify the deployed
// policy. This constructor does not connect or change the Topaz directory.
//
// The policy receives input.resource with tenant, actor, action and resource
// fields. Resource attributes remain nested beneath resource.attributes so they
// cannot replace the trusted actor, tenant or action. User and system identity
// mode is MANUAL: input.identity carries the actor ID, while input.user is empty.
// Public actors use NONE, leaving identity resolution disabled. Policies may
// use these application-supplied facts without requiring a seeded directory
// identity. Relationship policies still need their own directory provisioning.
func New(client authorizer.AuthorizerClient, options Options) (tenancy.Policy, error) {
	if client == nil {
		return nil, errors.New("topaz: Topaz client is required")
	}
	if strings.TrimSpace(options.Path) == "" || strings.TrimSpace(options.Revision) == "" {
		return nil, errors.New("topaz: Topaz policy path and configured revision are required")
	}
	if options.Decision == "" {
		options.Decision = "allowed"
	} else if strings.TrimSpace(options.Decision) == "" {
		return nil, errors.New("topaz: Topaz decision must not be blank")
	}
	if options.Timeout < 0 {
		return nil, errors.New("topaz: Topaz timeout must not be negative")
	}
	if options.Timeout == 0 {
		options.Timeout = 2 * time.Second
	}
	return &topazPolicy{client: client, options: options}, nil
}

func (p *topazPolicy) Decide(ctx context.Context, request tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	if err := request.Validate(); err != nil {
		return tenancy.PolicyDecision{}, err
	}
	resource, err := structpb.NewStruct(map[string]any{
		"tenant": map[string]any{
			"id":       request.Tenant.ID.String(),
			"slug":     request.Tenant.Slug,
			"operator": request.Tenant.Operator,
		},
		"actor": map[string]any{
			"kind": string(request.Actor.Kind),
			"id":   request.Actor.ID,
		},
		"action": request.Action,
		"resource": map[string]any{
			"tenant_id":  request.Resource.TenantID.String(),
			"kind":       request.Resource.Kind,
			"id":         request.Resource.ID,
			"attributes": request.Resource.Attributes,
		},
	})
	if err != nil {
		return tenancy.PolicyDecision{}, fmt.Errorf("topaz: encode Topaz policy facts: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, p.options.Timeout)
	defer cancel()
	identityType := api.IdentityType_IDENTITY_TYPE_MANUAL
	if request.Actor.Kind == tenancy.PolicyPublic {
		identityType = api.IdentityType_IDENTITY_TYPE_NONE
	}
	response, err := p.client.Is(ctx, &authorizer.IsRequest{
		IdentityContext: &api.IdentityContext{
			Type:     identityType,
			Identity: request.Actor.ID,
		},
		PolicyContext: &api.PolicyContext{
			Path:      p.options.Path,
			Decisions: []string{p.options.Decision},
		},
		ResourceContext: resource,
	})
	if err != nil {
		return tenancy.PolicyDecision{}, fmt.Errorf("topaz: evaluate Topaz policy: %w", err)
	}
	decisions := response.GetDecisions()
	if len(decisions) != 1 || decisions[0].GetDecision() != p.options.Decision {
		return tenancy.PolicyDecision{}, errors.New("topaz: Topaz returned an unexpected policy decision")
	}
	// Is returns a boolean, not a policy explanation. Reason only names the
	// evaluated decision; the policy's actual trace belongs in decision logs.
	return tenancy.PolicyDecision{
		Allowed:  decisions[0].GetIs(),
		Reason:   p.options.Decision,
		Revision: p.options.Revision,
	}, nil
}
