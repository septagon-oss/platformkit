package tenancy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type decisionFunc func(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error)

func (f decisionFunc) Decide(ctx context.Context, r tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	return f(ctx, r)
}

func TestPolicyCannotDecideAcrossTenantsOrInventAnActor(t *testing.T) {
	tenant := tenancy.Tenant{ID: uuid.New()}
	valid := tenancy.PolicyRequest{Tenant: tenant, Actor: tenancy.PolicyActor{Kind: tenancy.PolicyUser, ID: uuid.NewString()},
		Action: "album:publish", Resource: tenancy.PolicyResource{TenantID: tenant.ID, Kind: "album", ID: uuid.NewString()}}
	for _, change := range []struct {
		name string
		edit func(*tenancy.PolicyRequest)
	}{
		{"foreign resource", func(r *tenancy.PolicyRequest) { r.Resource.TenantID = uuid.New() }},
		{"missing tenant", func(r *tenancy.PolicyRequest) { r.Tenant.ID = uuid.Nil; r.Resource.TenantID = uuid.Nil }},
		{"implicit system actor", func(r *tenancy.PolicyRequest) { r.Actor = tenancy.PolicyActor{} }},
		{"anonymous user", func(r *tenancy.PolicyRequest) { r.Actor.ID = "" }},
		{"public impersonation", func(r *tenancy.PolicyRequest) { r.Actor.Kind = tenancy.PolicyPublic }},
	} {
		t.Run(change.name, func(t *testing.T) {
			r := valid
			change.edit(&r)
			called := false
			_, err := tenancy.RequirePolicy(t.Context(), decisionFunc(func(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
				called = true
				return tenancy.PolicyDecision{Allowed: true}, nil
			}), r)
			if !errors.Is(err, tenancy.ErrInvalidPolicyRequest) || called {
				t.Fatalf("invalid authority reached the provider: called=%v err=%v", called, err)
			}
		})
	}
}

func TestPolicyDistinguishesDenialFromAnUnavailableDecision(t *testing.T) {
	id := uuid.New()
	r := tenancy.PolicyRequest{Tenant: tenancy.Tenant{ID: id}, Actor: tenancy.PolicyActor{Kind: tenancy.PolicySystem, ID: "album-publisher"},
		Action: "album:publish", Resource: tenancy.PolicyResource{TenantID: id, Kind: "album"}}
	for _, tc := range []struct {
		name     string
		decision tenancy.PolicyDecision
		err      error
		want     error
	}{
		{"deny", tenancy.PolicyDecision{Reason: "not-assigned", Revision: "v2"}, nil, tenancy.ErrPolicyDenied},
		{"unavailable", tenancy.PolicyDecision{Allowed: true}, context.DeadlineExceeded, tenancy.ErrPolicyUnavailable},
		{"provider returned denial error", tenancy.PolicyDecision{}, tenancy.ErrPolicyDenied, tenancy.ErrPolicyUnavailable},
		{"allow", tenancy.PolicyDecision{Allowed: true, Revision: "v2"}, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tenancy.RequirePolicy(t.Context(), decisionFunc(func(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
				return tc.decision, tc.err
			}), r)
			if !errors.Is(err, tc.want) {
				t.Fatalf("decision error = %v, want %v", err, tc.want)
			}
			if tc.err != nil && got.Allowed {
				t.Fatal("an unavailable provider retained an allow")
			}
			if tc.err == nil && got != tc.decision {
				t.Fatalf("lost decision evidence: %+v", got)
			}
		})
	}
	if _, err := tenancy.RequirePolicy(t.Context(), nil, r); !errors.Is(err, tenancy.ErrPolicyUnavailable) {
		t.Fatalf("missing provider = %v", err)
	}
}
