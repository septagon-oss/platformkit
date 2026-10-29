package httpx_test

// a_declaration_that_names_no_permission_refuses_a_host_that_resolves_to_no_tenant_test.go
// asks one tenant-less door of every closure a registration supplies for a
// resource whose guard names no permission.
//
// A resource guarded by httpx.SignedIn() names no grant, so its closures are
// guarded by the half of the declaration that says who the caller is rather than
// what the caller holds. That half states the two placement refusals again, in
// this package's own words, beside the ones `may` states for a declaration that
// does name a grant: a request whose host resolved to no tenant is tenant work
// with nobody's rows behind it, and a request nobody recognises has nobody to be.
// The two copies are droppable independently, and the second is what a door that
// admits every credential would otherwise answer with — the ceiling rule and the
// undeclared-kind rule, neither of which is about the tenant at all. So a caller
// the installation recognises, standing in a request whose host resolved to no
// tenant, would reach a module's own closure with no tenant in hand and read or
// write whatever that closure does when handed nothing. The refusal that stands
// in front of that is the tenant's, not the caller's, and it is asked first:
// signing in answers the anonymous refusal and changes nothing about the host.
//
// This asks it of every closure a registration supplies — the list, the total,
// the two narrowed totals, the single record and the three writes — because each
// is wrapped on its own, and refuses a recognised caller in a request with no
// tenant at each, owing 403 naming the no-tenant code with nothing run and the
// Authorizer never consulted: a declaration that names no permission has no grant
// to ask about. The control is the same caller, in a tenant, served at every door.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestADeclarationThatNamesNoPermissionRefusesAHostThatResolvesToNoTenant
// registers a resource whose read and write guards are httpx.SignedIn(), reads
// the guarded closures back off the registration, and asks each of them as a
// recognised caller in a request that resolved to no tenant. Every door owes 403
// naming the no-tenant code, having run nothing and asked the Authorizer nothing.
// The same caller, in a tenant, is served at each door once.
func TestADeclarationThatNamesNoPermissionRefusesAHostThatResolvesToNoTenant(t *testing.T) {
	api, _, f := setup(t)

	day := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	id := uuid.New()
	ran := map[string]int{}
	// Nothing is refused on a grant here, so a grant that always answers is what
	// makes the door's own refusal the only refusal a caller can meet.
	f.allow = true
	api.RegisterResource(httpx.Resource{
		Module: "billing", Entity: "seat",
		ReadBy:  httpx.SignedIn(),
		WriteBy: httpx.SignedIn(),
		List: func(context.Context, crud.Query) ([]map[string]any, int64, error) {
			ran["List"]++
			return []map[string]any{{"seat": 1}}, 4, nil
		},
		Count: func(context.Context) (int64, error) {
			ran["Count"]++
			return 4, nil
		},
		CountFiltered: func(context.Context, crud.Where) (int64, error) {
			ran["CountFiltered"]++
			return 2, nil
		},
		CountDays: func(context.Context, string, time.Time, time.Time) ([]crud.DayCount, error) {
			ran["CountDays"]++
			return []crud.DayCount{{Day: day, Count: 1}}, nil
		},
		Get: func(context.Context, uuid.UUID) (map[string]any, error) {
			ran["Get"]++
			return map[string]any{"seat": 1}, nil
		},
		Create: func(context.Context, map[string]any) (map[string]any, error) {
			ran["Create"]++
			return map[string]any{"seat": 2}, nil
		},
		Update: func(context.Context, uuid.UUID, map[string]any) (map[string]any, error) {
			ran["Update"]++
			return map[string]any{"seat": 3}, nil
		},
		Delete: func(context.Context, uuid.UUID) error {
			ran["Delete"]++
			return nil
		},
	})
	door := httpx.Resource{}
	for _, r := range api.Resources() {
		if r.Module == "billing" && r.Entity == "seat" {
			door = r
		}
	}
	if door.CountFiltered == nil || door.Get == nil || door.Create == nil {
		t.Fatal("registration supplied no guarded closure for a resource that writes them: a door with nothing behind it is not a door")
	}

	doors := []struct {
		name string
		call func(context.Context) error
	}{
		{"List", func(ctx context.Context) error { _, _, err := door.List(ctx, crud.Query{Limit: 10}); return err }},
		{"Count", func(ctx context.Context) error { _, err := door.Count(ctx); return err }},
		{"CountFiltered", func(ctx context.Context) error { _, err := door.CountFiltered(ctx, crud.Where{}); return err }},
		{"CountDays", func(ctx context.Context) error {
			_, err := door.CountDays(ctx, "created_at", day, day)
			return err
		}},
		{"Get", func(ctx context.Context) error { _, err := door.Get(ctx, id); return err }},
		{"Create", func(ctx context.Context) error { _, err := door.Create(ctx, map[string]any{"seat": 2}); return err }},
		{"Update", func(ctx context.Context) error {
			_, err := door.Update(ctx, id, map[string]any{"seat": 3})
			return err
		}},
		{"Delete", func(ctx context.Context) error { return door.Delete(ctx, id) }},
	}

	caller := tenancy.Principal{UserID: uuid.New(), Roles: []string{"member"}}

	// The caller the installation recognises, in a request whose host resolved to
	// no tenant. The door cannot name a row set to read, so it refuses before it
	// asks anything about the caller — and signing in would not help.
	for _, d := range doors {
		ran[d.name] = 0
		f.asked.Store(0)
		err := d.call(t.Context())
		var refused *problem.Problem
		if !errors.As(err, &refused) {
			t.Errorf("%s answered a recognised caller with no tenant at all with %v: the guard in front of this closure owes the request the refusal that fits a host resolving to none",
				d.name, err)
			continue
		}
		if refused.Status != http.StatusForbidden || !strings.Contains(refused.Detail, httpx.CodeNoTenant) {
			t.Errorf("%s answered a recognised caller with no tenant at all with %d %q, want 403 naming %s: which of the two placement refusals this is decides whether signing in helps, and it does not",
				d.name, refused.Status, refused.Detail, httpx.CodeNoTenant)
		}
		if ran[d.name] != 0 {
			t.Errorf("%s ran its own closure %d time(s) for a request with no tenant, want not at all: handed no tenant, the work behind this door is whatever the module does with none",
				d.name, ran[d.name])
		}
		if got := f.asked.Load(); got != 0 {
			t.Errorf("%s asked the authorizer %d time(s) for a request it refused for want of a tenant, want never: this declaration names no permission, so no grant was ever in question",
				d.name, got)
		}
	}

	// The control: the same caller, in a tenant, served at every door. Without it
	// the refusals above would only have shown doors that answer nobody.
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
