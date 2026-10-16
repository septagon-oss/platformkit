// Package opa adapts Open Policy Agent, embedded as a library, to tenancy.Policy.
//
// The register (decision 0052) names OPA for object-scope authorization: a composition
// writes its rules in Rego, the open standard policy language, and they run in process —
// no policy server to deploy, reach or keep in step. The Topaz adapter beside this one is
// the choice for a composition that wants a directory of relations served elsewhere.
//
// A policy module answers `decision` in the package it declares, as an object:
//
//	package platformkit.task
//	decision := {"allow": true, "reason": "..."} if { ... }
//
// with the tenancy.PolicyRequest as `input`: `input.tenant.id`, `input.tenant.slug`,
// `input.actor.kind`, `input.actor.id`, `input.action`, `input.resource.kind`,
// `input.resource.id`, `input.resource.attributes`. An undefined decision is a denial,
// and anything that is not an object with a boolean `allow` is an outage — never an
// allow — because tenancy.RequirePolicy treats a provider failure as a refusal.
//
// Revision is `sha256:` and the first twelve hex characters of the policy source, so a
// decision stored beside the record it authorized names the exact rules that made it.
package opa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/open-policy-agent/opa/v1/rego"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Policy is one compiled policy module.
type Policy struct {
	query    rego.PreparedEvalQuery
	revision string
}

var _ tenancy.Policy = (*Policy)(nil)

// New compiles source, a Rego module declaring package pkg, once. file names it in the
// compiler's messages.
func New(ctx context.Context, file, pkg, source string) (*Policy, error) {
	if strings.TrimSpace(pkg) == "" {
		return nil, fmt.Errorf("opa: a policy names its package")
	}
	query, err := rego.New(
		rego.Query("data."+pkg+".decision"),
		rego.Module(file, source),
	).PrepareForEval(ctx)
	if err != nil {
		return nil, fmt.Errorf("opa: compile %s: %w", file, err)
	}
	sum := sha256.Sum256([]byte(source))
	return &Policy{query: query, revision: "sha256:" + hex.EncodeToString(sum[:])[:12]}, nil
}

// MustNew is New for a policy embedded in the binary, where a compile error is a defect
// in the build rather than a condition to handle at run time.
func MustNew(file, pkg, source string) *Policy {
	p, err := New(context.Background(), file, pkg, source)
	if err != nil {
		panic(err)
	}
	return p
}

// Revision is the content hash of the policy this evaluates.
func (p *Policy) Revision() string { return p.revision }

// Decide evaluates one request.
func (p *Policy) Decide(ctx context.Context, r tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	attributes := r.Resource.Attributes
	if attributes == nil {
		attributes = map[string]any{}
	}
	input := map[string]any{
		"tenant": map[string]any{"id": r.Tenant.ID.String(), "slug": r.Tenant.Slug},
		"actor":  map[string]any{"kind": string(r.Actor.Kind), "id": r.Actor.ID},
		"action": r.Action,
		"resource": map[string]any{
			"kind": r.Resource.Kind, "id": r.Resource.ID, "attributes": attributes,
		},
	}
	results, err := p.query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return tenancy.PolicyDecision{}, fmt.Errorf("%w: opa: %v", tenancy.ErrPolicyUnavailable, err)
	}
	if len(results) == 0 || len(results[0].Expressions) == 0 {
		return tenancy.PolicyDecision{Allowed: false, Reason: "no rule allows " + r.Action, Revision: p.revision}, nil
	}
	decision, ok := results[0].Expressions[0].Value.(map[string]any)
	if !ok {
		return tenancy.PolicyDecision{}, fmt.Errorf("%w: opa: the decision is %T, not an object", tenancy.ErrPolicyUnavailable, results[0].Expressions[0].Value)
	}
	allowed, ok := decision["allow"].(bool)
	if !ok {
		return tenancy.PolicyDecision{}, fmt.Errorf("%w: opa: the decision carries no boolean allow", tenancy.ErrPolicyUnavailable)
	}
	reason, _ := decision["reason"].(string)
	return tenancy.PolicyDecision{Allowed: allowed, Reason: reason, Revision: p.revision}, nil
}
