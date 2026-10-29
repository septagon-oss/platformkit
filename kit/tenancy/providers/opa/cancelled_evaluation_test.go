package opa_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/opa"
)

// TestACancelledEvaluationIsAnOutageNeverAnAllow: a caller whose context is already
// done gets an unavailable error and no allowing decision, even when the rules allow.
func TestACancelledEvaluationIsAnOutageNeverAnAllow(t *testing.T) {
	p, err := opa.New(t.Context(), "owned.rego", "test.owned", owned)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for range 50 {
		d, err := p.Decide(ctx, request("doc:edit", "ann", "ann"))
		if !errors.Is(err, tenancy.ErrPolicyUnavailable) || d.Allowed {
			t.Fatalf("decision = %+v, err = %v", d, err)
		}
	}
}
