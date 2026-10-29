package httpx_test

// a_caller_whose_request_resolved_to_no_tenant_is_never_handed_a_closure_test.go
// asks what a guarded closure does with a caller it can name in a request whose
// host resolved to no tenant.
//
// A closure's guard refuses a request that resolved to no tenant before it asks
// anything about the caller, and one reason for that order is that a closure is
// never handed a request without a tenant to work in: the work behind it is
// whatever the module does with whatever it was given. `recognised` states that
// promise directly — a principal carried by a request with no tenant is not a
// caller anybody, so a guard that got past its placement refusal would still
// answer it the anonymous way rather than run the work. Nothing asks that of a
// context which does carry a principal: the tenantless door is only ever opened
// with an empty one, so the second half of the promise — a caller somebody *is*
// still reaches no closure — rests on `recognised`'s tenant check alone.
//
// So this registers a resource whose guards name no permission, reads its
// closures back off the registration, and opens the read door and each write door
// with a principal and no tenant. The answer owed is the tenant's refusal — 403
// naming the no-tenant code, which signing in does not answer — and the closure
// runs neither time, so the door is closed both by the refusal in front of it and
// by what a tenantless request is not allowed to become. The control is the same
// principal in the tenant the host resolved to, served at every door.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestACallerWhoseRequestResolvedToNoTenantIsNeverHandedAClosure registers a
// resource guarded by httpx.SignedIn(), which names no grant, and asks each
// closure the registration supplies as a caller the installation would recognise
// in a request that resolved to no tenant. Every door owes 403 naming the
// no-tenant code, has run nothing and asked the Authorizer nothing; the same
// caller inside the resolved tenant is served at each door once.
func TestACallerWhoseRequestResolvedToNoTenantIsNeverHandedAClosure(t *testing.T) {
	api, _, f := setup(t)

	id := uuid.New()
	ran := map[string]int{}
	// No grant is ever in question here, so an Authorizer that always agrees is
	// what keeps a 403 from being mistaken for a denial of permission.
	f.allow = true
	api.RegisterResource(httpx.Resource{
		Module: "billing", Entity: "chair",
		ReadBy:  httpx.SignedIn(),
		WriteBy: httpx.SignedIn(),
		List: func(context.Context, crud.Query) ([]map[string]any, int64, error) {
			ran["List"]++
			return []map[string]any{{"chair": 1}}, 4, nil
		},
		Get: func(context.Context, uuid.UUID) (map[string]any, error) {
			ran["Get"]++
			return map[string]any{"chair": 1}, nil
		},
		Create: func(context.Context, map[string]any) (map[string]any, error) {
			ran["Create"]++
			return map[string]any{"chair": 2}, nil
		},
		Update: func(context.Context, uuid.UUID, map[string]any) (map[string]any, error) {
			ran["Update"]++
			return map[string]any{"chair": 3}, nil
		},
		Delete: func(context.Context, uuid.UUID) error {
			ran["Delete"]++
			return nil
		},
	})
	door := httpx.Resource{}
	for _, r := range api.Resources() {
		if r.Module == "billing" && r.Entity == "chair" {
			door = r
		}
	}
	if door.List == nil || door.Update == nil || door.Delete == nil {
		t.Fatal("registration supplied no guarded closure for a resource that writes them: a door with nothing behind it is not a door")
	}

	doors := []struct {
		name string
		call func(context.Context) error
	}{
		{"List", func(ctx context.Context) error { _, _, err := door.List(ctx, crud.Query{Limit: 10}); return err }},
		{"Get", func(ctx context.Context) error { _, err := door.Get(ctx, id); return err }},
		{"Create", func(ctx context.Context) error { _, err := door.Create(ctx, map[string]any{"chair": 2}); return err }},
		{"Update", func(ctx context.Context) error {
			_, err := door.Update(ctx, id, map[string]any{"chair": 3})
			return err
		}},
		{"Delete", func(ctx context.Context) error { return door.Delete(ctx, id) }},
	}

	caller := tenancy.Principal{UserID: uuid.New(), Roles: []string{"member"}}

	// A caller somebody is, in a request whose host resolved to no tenant. The
	// refusal owed is the tenant's, not the caller's: signing in answers an
	// anonymous refusal and changes nothing about the host. And the work behind
	// the door stays unrun, because a request with no tenant is never handed to it.
	for _, d := range doors {
		ran[d.name] = 0
		f.asked.Store(0)
		carrier := tenancy.WithPrincipal(t.Context(), caller)
		if _, hasTenant := tenancy.FromContext(carrier); hasTenant {
			t.Fatalf("%s: the test handed the door a context with a tenant in it, so it asks nothing", d.name)
		}
		err := d.call(carrier)
		var refused *problem.Problem
		if !errors.As(err, &refused) {
			t.Errorf("%s answered a caller it could name in a request that resolved to no tenant with %v: the guard owes this request the refusal that fits a host resolving to none",
				d.name, err)
			continue
		}
		if refused.Status != http.StatusForbidden || !strings.Contains(refused.Detail, httpx.CodeNoTenant) {
			t.Errorf("%s answered a caller it could name in a request that resolved to no tenant with %d %q, want 403 naming %s: the host is what resolved to none, and no credential fixes that",
				d.name, refused.Status, refused.Detail, httpx.CodeNoTenant)
		}
		if ran[d.name] != 0 {
			t.Errorf("%s ran its own closure %d time(s) for a request with no tenant, want not at all: a guard that reached its work in a request with no tenant does whatever the module does with none",
				d.name, ran[d.name])
		}
		if got := f.asked.Load(); got != 0 {
			t.Errorf("%s asked the authorizer %d time(s) for a request it refused for want of a tenant, want never: this declaration names no permission, so no grant was ever in question",
				d.name, got)
		}
	}

	// The control: the same caller, in the tenant the host resolved to, served at
	// every door. Without it the refusals above would only have shown doors that
	// answer nobody.
	inTenant := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), f.tenant), caller)
	for _, d := range doors {
		ran[d.name] = 0
		f.asked.Store(0)
		if err := d.call(inTenant); err != nil {
			t.Errorf("%s refused a recognised caller in the tenant the host resolved to: %v", d.name, err)
			continue
		}
		if ran[d.name] != 1 {
			t.Errorf("%s ran its own closure %d time(s) for a caller it answered, want once", d.name, ran[d.name])
		}
	}
}
