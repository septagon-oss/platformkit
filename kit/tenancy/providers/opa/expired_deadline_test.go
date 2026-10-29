package opa_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/opa"
)

// TestAnExpiredDeadlineIsAnOutageNeverAnAllow: a caller whose deadline has passed gets
// an unavailable error and no allowing decision, even when the rules allow.
func TestAnExpiredDeadlineIsAnOutageNeverAnAllow(t *testing.T) {
	p, err := opa.New(t.Context(), "owned.rego", "test.owned", owned)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	d, err := p.Decide(ctx, request("doc:edit", "ann", "ann"))
	if !errors.Is(err, tenancy.ErrPolicyUnavailable) || d.Allowed {
		t.Fatalf("decision = %+v, err = %v", d, err)
	}
}
