package httpx_test

// each_half_of_a_declaration_runs_no_module_work_for_a_request_with_no_tenant_test.go
// asks the quieter half of what the placement refusals are for: whatever a guarded
// closure answers to a request whose host resolved to no tenant, it answers before
// any of the module's work runs, and it hands back nothing.
//
// A declaration guards its closures in two halves. The half that names a permission
// asks the Authorizer about the caller; the half that names none — httpx.SignedIn()
// and its siblings — has no grant to ask about. Each refuses a request with no tenant
// in its own words, and the cases beside this one pin which refusal answers and under
// which code. Which code is not asked here, because no rewording should move what this
// asks: a closure that counted a tenant's rows and refused them afterwards would still
// print the right status at the address, and a total returned beside a refusal is a
// card full of numbers nobody was shown. The work and the answer are the promise.
//
// So this registers one resource per half, reads both closures back off the
// registration, and calls each as a principal carried by a context that resolved to no
// tenant: a refusal, no rows, no total, nothing run, nothing asked of the Authorizer.
// The control serves the same caller inside the tenant the host resolved to, which is
// also where the two halves part company — the half that names a permission asks the
// Authorizer once there, the half that names none never asks it at all.

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestEachHalfOfADeclarationRunsNoModuleWorkForARequestWithNoTenant registers a
// resource guarded by a named permission and one guarded by httpx.SignedIn(), and
// calls each read closure as a recognised caller in a request that resolved to no
// tenant. Each owes a 4xx, no rows, no total, no run of the closure and no question
// asked of the Authorizer; the same caller inside the resolved tenant is served once,
// the named permission asking the Authorizer once and the unnamed guard never.
func TestEachHalfOfADeclarationRunsNoModuleWorkForARequestWithNoTenant(t *testing.T) {
	api, _, f := setup(t)
	// An Authorizer that always agrees is what keeps a refusal from being
	// mistaken for a denial of the permission the first half names.
	f.allow = true

	ran := map[string]int{}
	halves := []struct {
		entity string
		read   httpx.Auth
		ask    int32 // questions the Authorizer is owed once the caller is placed
	}{
		{"desk", httpx.Permission("desk:read"), 1},
		{"shelf", httpx.SignedIn(), 0},
	}
	for _, h := range halves {
		ran[h.entity] = 0
		entity := h.entity
		api.RegisterResource(httpx.Resource{
			Module: "billing", Entity: entity,
			ReadBy: h.read,
			List: func(context.Context, crud.Query) ([]map[string]any, int64, error) {
				ran[entity]++
				return []map[string]any{{"row": 1}}, 7, nil
			},
		})
	}
	door := map[string]httpx.Resource{}
	for _, r := range api.Resources() {
		if _, wanted := ran[r.Entity]; wanted {
			door[r.Entity] = r
		}
	}
	if len(door) != len(halves) {
		t.Fatalf("the registration supplied %d of the %d guarded closures: a door with nothing behind it asks nothing", len(door), len(halves))
	}

	caller := tenancy.Principal{UserID: uuid.New(), Roles: []string{"member"}}
	for _, h := range halves {
		ran[h.entity] = 0
		f.asked.Store(0)
		ctx := tenancy.WithPrincipal(t.Context(), caller)
		if _, hasTenant := tenancy.FromContext(ctx); hasTenant {
			t.Fatalf("%s: the test handed the door a context with a tenant in it, so it asks nothing", h.entity)
		}
		rows, total, err := door[h.entity].List(ctx, crud.Query{Limit: 10})
		if err == nil {
			t.Errorf("%s answered a recognised caller in a request that resolved to no tenant with %d rows, %d and no error: this is tenant work with nobody's rows to show",
				h.entity, len(rows), total)
		} else {
			var refused *problem.Problem
			if !errors.As(err, &refused) {
				t.Errorf("%s answered that caller with the non-problem error %v: a door that refuses owes the caller a refusal they can read", h.entity, err)
			} else if refused.Status < http.StatusBadRequest || refused.Status > 499 {
				t.Errorf("%s answered that caller with %d, want a 4xx. Which of the two placement refusals answers, and under what code, is asked beside this file; this asks only that the door stayed shut",
					h.entity, refused.Status)
			}
		}
		if len(rows) != 0 || total != 0 {
			t.Errorf("%s answered %d rows and a total of %d for a request with no tenant, want neither beside any answer: a total with no error in front of it is a card of another tenant's counts",
				h.entity, len(rows), total)
		}
		if ran[h.entity] != 0 {
			t.Errorf("%s ran the module's closure %d time(s) for a request with no tenant, want not at all: the refusal is owed before the work, because the work is what a request with no tenant may not spend",
				h.entity, ran[h.entity])
		}
		if got := f.asked.Load(); got != 0 {
			t.Errorf("%s asked the Authorizer %d time(s) for a request it refused for want of a tenant, want never: nobody was placed, so no grant was ever in question",
				h.entity, got)
		}
	}

	// The control: the same caller, inside the tenant the host resolved to. Here
	// the two halves differ in who they ask, and both serve.
	for _, h := range halves {
		ran[h.entity] = 0
		f.asked.Store(0)
		inTenant := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), f.tenant), caller)
		rows, total, err := door[h.entity].List(inTenant, crud.Query{Limit: 10})
		if err != nil || len(rows) != 1 || total != 7 || ran[h.entity] != 1 || f.asked.Load() != h.ask {
			t.Errorf("%s answered a recognised caller in the tenant the host resolved to with %d rows, %d, %d run(s), %d question(s) and %v, want 1 row, 7, 1 run and %d question(s): a door that only answers a request it refused is not guarded, it is closed",
				h.entity, len(rows), total, ran[h.entity], f.asked.Load(), err, h.ask)
		}
	}
}
