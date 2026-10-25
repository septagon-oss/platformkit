package httpx_test

// A typed query's closure is a read of the resource that declares it, so it wears
// that resource's read guard from the moment it is registered — the way List, Get,
// Count and the version series do. `mayDeclare` exists because a hand-written page
// or a catalogue card holds an `httpx.Resource` and calls its closures in process,
// beneath whatever route it happens to stand in front of; the in-process answer has
// to be the answer the address gives, for every closure the resource carries.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAResourcesInputDoorAsksWhatItsRouteAsks(t *testing.T) {
	api, _, f := setup(t)
	f.allow = true
	ran := 0
	api.RegisterResource(httpx.Resource{
		Module: "billing", Entity: "estimate", ReadBy: httpx.SignedIn(),
		InputRun: func(context.Context, map[string]any) (any, error) {
			ran++
			return map[string]any{"answer": "42"}, nil
		},
	})
	var query httpx.Resource
	for _, r := range api.Resources() {
		if r.Entity == "estimate" {
			query = r
		}
	}
	if query.InputRun == nil {
		t.Fatal("the API recorded no typed query closure")
	}
	ask := func(p *tenancy.Principal) (any, int, error) {
		t.Helper()
		before := ran
		ctx := tenancy.WithTenant(t.Context(), f.tenant)
		if p != nil {
			ctx = tenancy.WithPrincipal(ctx, *p)
		}
		out, err := query.InputRun(ctx, nil)
		return out, ran - before, err
	}

	// Nobody signed in: the address of this query refuses, so its closure must
	// refuse too, and must not have run. A closure that answered here would read
	// past the permission the route stands behind for whoever calls it in process.
	out, runs, err := ask(nil)
	if err == nil || !strings.Contains(err.Error(), httpx.CodeAnonymous) {
		t.Errorf("an unrecognised caller through the typed query = %v (%v), want a refusal naming %s",
			err, out, httpx.CodeAnonymous)
	}
	var refusal *problem.Problem
	if !errors.As(err, &refusal) || refusal.Status != http.StatusUnauthorized {
		t.Errorf("an unrecognised caller through the typed query = %v, want 401: nobody has signed in yet, which the route answers too", err)
	}
	if runs != 0 {
		t.Errorf("that refusal ran the typed query %d times, want never", runs)
	}

	// The caller the declaration admits is served, once, with its answer: the
	// guard is the resource's read, not a wall around the feature.
	if out, runs, err := ask(&tenancy.Principal{UserID: uuid.New()}); err != nil || runs != 1 || out == nil {
		t.Errorf("a signed-in caller through the typed query = %v after %d runs, want an answer after one: %v", out, runs, err)
	}
}
