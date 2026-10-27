// Package opa adapts Open Policy Agent, embedded as a library, to tenancy.Policy.
//
// The register (decision 0052) names OPA for object-scope authorization: a
// composition writes its rules in Rego and they run in process, with no policy
// server to deploy or keep in step; Topaz is the choice beside this one when the
// relations are served elsewhere.
//
// A policy module answers `decision` in the package it declares, as an object:
//
//	package platformkit.task
//	decision := {"allow": true, "reason": "..."} if { ... }
//
// with the tenancy.PolicyRequest as `input`. An undefined decision is a denial,
// and anything but an object with a boolean allow is an outage — never an allow.
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
	if err := ctx.Err(); err != nil {
		return tenancy.PolicyDecision{}, fmt.Errorf("%w: opa: %w", tenancy.ErrPolicyUnavailable, err)
	}
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
	if err == nil {
		err = ctx.Err()
	}
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
