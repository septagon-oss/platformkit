package tenancy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Policy evaluates an application use case using facts loaded by its owner.
// It does not replace tenant isolation, current account checks or domain rules.
type Policy interface {
	Decide(context.Context, PolicyRequest) (PolicyDecision, error)
}

type PolicyActorKind string

const (
	PolicyUser   PolicyActorKind = "user"
	PolicySystem PolicyActorKind = "system"
	PolicyPublic PolicyActorKind = "public"
)

// PolicyActor makes platform work and intentional public access explicit.
// An absent user is never implicitly promoted to a system actor.
type PolicyActor struct {
	Kind PolicyActorKind
	ID   string
}

// PolicyResource is a tenant-qualified object or creation/collection scope.
// An empty ID identifies the latter, not all objects of this kind. Attributes
// are trusted application facts, not an unvalidated request body.
type PolicyResource struct {
	TenantID   uuid.UUID
	Kind       string
	ID         string
	Attributes map[string]any
}

type PolicyRequest struct {
	Tenant   Tenant
	Actor    PolicyActor
	Action   string
	Resource PolicyResource
}

// PolicyDecision describes one evaluation. Revision is populated only when the
// provider integration can identify its policy artifact; it is not a guarantee
// of atomicity between a directory decision and a database transaction.
type PolicyDecision struct {
	Allowed  bool
	Reason   string
	Revision string
}

var (
	ErrInvalidPolicyRequest = errors.New("policy: invalid decision request")
	ErrPolicyDenied         = errors.New("policy: access denied")
	ErrPolicyUnavailable    = errors.New("policy: decision unavailable")
)

func (r PolicyRequest) Validate() error {
	if r.Tenant.ID == uuid.Nil || r.Resource.TenantID != r.Tenant.ID {
		return fmt.Errorf("%w: resource and request must belong to the same tenant", ErrInvalidPolicyRequest)
	}
	if strings.TrimSpace(r.Action) == "" || strings.TrimSpace(r.Resource.Kind) == "" {
		return fmt.Errorf("%w: action and resource kind are required", ErrInvalidPolicyRequest)
	}
	switch r.Actor.Kind {
	case PolicyUser, PolicySystem:
		if strings.TrimSpace(r.Actor.ID) == "" {
			return fmt.Errorf("%w: user and system actors require an identity", ErrInvalidPolicyRequest)
		}
	case PolicyPublic:
		if r.Actor.ID != "" {
			return fmt.Errorf("%w: public access carries no identity", ErrInvalidPolicyRequest)
		}
	default:
		return fmt.Errorf("%w: declare a user, system or public actor", ErrInvalidPolicyRequest)
	}
	return nil
}

// RequirePolicy is the common refusal boundary for services and workers. Call
// it with current, tenant-scoped facts before mutating state or claiming work.
// A nil provider and provider failure are outages, never permissive defaults.
func RequirePolicy(ctx context.Context, policy Policy, request PolicyRequest) (PolicyDecision, error) {
	if err := request.Validate(); err != nil {
		return PolicyDecision{}, err
	}
	if policy == nil {
		return PolicyDecision{}, ErrPolicyUnavailable
	}
	decision, err := policy.Decide(ctx, request)
	if err != nil {
		return PolicyDecision{}, fmt.Errorf("%w: %w", ErrPolicyUnavailable, err)
	}
	if !decision.Allowed {
		return decision, ErrPolicyDenied
	}
	return decision, nil
}
