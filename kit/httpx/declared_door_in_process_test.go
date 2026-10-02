package httpx_test

// The declaration a resource carries is asked in two places: by the middleware at the
// address, and in process by `mayDeclare`, because a hand-written page, a dashboard card
// and the catalogue all hold an `httpx.Resource` and call its closures beneath whatever
// route they happen to stand in front of. Those two answers have to be one answer.
//
// The fifth declaration — `AnyCredential`, which names no permission and spends none of
// its caller's authority — and the rule that a credential carrying its own ceiling may not
// stand in for a `SignedIn` operation both arrived in the kernel while this operation-set
// change was open. Read without this file, the in-process door kept admitting what the
// route refuses, or kept refusing what the route admits: a resource readable through its
// `List` closure by a key that its own address turns away, which is the ceiling read only
// where someone remembered to read it.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// declaredDoors records a read closure per declaration and returns them keyed by entity,
// with the number of times each closure's body ran.
func declaredDoors(t *testing.T, api *httpx.API, f *fixture) (map[string]httpx.Resource, map[string]int) {
	t.Helper()
	f.allow = true
	ran := map[string]int{}
	doors := map[string]httpx.Resource{}
	for _, d := range []struct {
		entity string
		auth   httpx.Auth
	}{{"holder", httpx.SignedIn()}, {"described", httpx.AnyCredential()}} {
		ran[d.entity] = 0
		runs := d.entity
		api.RegisterResource(httpx.Resource{
			Module: "billing", Entity: runs,
			ReadBy: d.auth,
			List: func(ctx context.Context, _ crud.Query) ([]map[string]any, int64, error) {
				ran[runs]++
				return nil, 0, nil
			},
		})
	}
	for _, r := range api.Resources() {
		if _, wanted := ran[r.Entity]; wanted {
			doors[r.Entity] = r
		}
	}
	if len(doors) != len(ran) {
		t.Fatalf("the API recorded %d of the %d resources registered", len(doors), len(ran))
	}
	return doors, ran
}

func TestAResourcesInProcessDoorAsksWhatItsRouteAsks(t *testing.T) {
	api, _, f := setup(t)
	doors, ran := declaredDoors(t, api, f)

	ask := func(entity string, p *tenancy.Principal) (int, error) {
		t.Helper()
		r, known := doors[entity]
		if !known {
			t.Fatalf("no resource recorded for %q", entity)
		}
		ctx := tenancy.WithTenant(t.Context(), f.tenant)
		if p != nil {
			ctx = tenancy.WithPrincipal(ctx, *p)
		}
		before := ran[entity]
		_, _, err := r.List(ctx, crud.Query{})
		return ran[entity] - before, err
	}

	key := &tenancy.Principal{UserID: uuid.New(), Permissions: []string{"billing:read"}}
	holder := &tenancy.Principal{UserID: uuid.New()}

	// The door that spends its caller's whole authority refuses a credential that
	// carries its own ceiling, in process exactly as the middleware does at the
	// address — and the refusal runs nothing.
	runs, err := ask("holder", key)
	if err == nil || !strings.Contains(err.Error(), httpx.CodeDenied) {
		t.Errorf("a key scoped to billing:read through a SignedIn resource's List = %v, want a refusal naming %s: "+
			"the operation spends its caller's whole authority, which is what a scope exists to stop, and the route "+
			"refuses it — a closure that admitted it would be the ceiling read only where someone remembered",
			err, httpx.CodeDenied)
	}
	if runs != 0 {
		t.Errorf("that refusal ran the read closure %d times, want never", runs)
	}

	// The door that spends none of it admits the same key. This is the credential a
	// client that is not a browser holds, and the resource catalogue is the address
	// it opens; refusing it in process would leave a working credential unable to do
	// the one thing the declaration says it is for.
	if runs, err := ask("described", key); err != nil || runs != 1 {
		t.Errorf("the same key through an AnyCredential resource's List = %v after %d runs, want nil after one: "+
			"the declaration names no permission and spends none, so there is nothing here for the scope to be refused for",
			err, runs)
	}

	// The session caller whose roles decide is served at both doors, which is the
	// control that says the refusal above is about the credential and not the kind.
	for _, entity := range []string{"holder", "described"} {
		ran[entity] = 0
		if runs, err := ask(entity, holder); err != nil || runs != 1 {
			t.Errorf("a caller whose roles decide through %q's List = %v after %d runs, want nil after one", entity, err, runs)
		}
	}

	// And nobody at all is refused at both, naming the anonymous code rather than the
	// scope: `any credential` is not every caller, and a door that admitted an
	// unrecognised request would be Public() with an extra step.
	for _, entity := range []string{"holder", "described"} {
		ran[entity] = 0
		runs, err := ask(entity, nil)
		if err == nil || !strings.Contains(err.Error(), httpx.CodeAnonymous) {
			t.Errorf("an unrecognised request through %q's List = %v, want a refusal naming %s", entity, err, httpx.CodeAnonymous)
		}
		if runs != 0 {
			t.Errorf("that refusal ran the read closure %d times, want never", runs)
		}
	}
}
