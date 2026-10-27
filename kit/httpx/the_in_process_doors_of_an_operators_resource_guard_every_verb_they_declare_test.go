package httpx_test

// the_in_process_doors_of_an_operators_resource_guard_every_verb_they_declare_test.go
// asks every closure one registered resource hands out — the five reads, the version
// series and the three writes — of the same operator declaration, in process.
//
// Registration wraps each verb in the same function, from one declaration, and that
// "each" is the whole promise a hand-written page and a derived form rely on: the
// closures are the doors a screen calls, so a verb that stopped asking would be a
// read or a write past the boundary that no route refuses. One verb left unguarded is
// invisible from the outside — the address still refuses, because the middleware asks
// for the route — and the cases that mount a resource over a router therefore cannot
// see it. The per-verb wrapping is visible only at this package, so every verb is
// asked here: the counts a dashboard card reads, the series a version panel reads, and
// the writes a generated form makes.
//
// The read and write permissions differ on purpose. The refusal quotes the permission
// it was asked about, so a door wrapped with its sibling's declaration — a Create that
// spends the read guard, a Get that spends the write one — fails here even though both
// refuse the same caller for the same reason. The installation's own pass is the
// control: without it every assertion below holds on a door that opened for nobody.

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

// TestTheInProcessDoorsOfAnOperatorsResourceGuardEveryVerbTheyDeclare mounts one
// resource whose reads carry OperatorRead and whose writes carry OperatorWrite, under
// an authorizer that grants everything so that only the declared surface can answer,
// and asks each of its nine closures as a customer's administrator and as the
// installation. Each door owes that first caller a 403 naming the operator boundary
// and the permission that door itself declares, having run nothing and asked the
// authorizer nothing; the installation is taken through all nine, once each.
func TestTheInProcessDoorsOfAnOperatorsResourceGuardEveryVerbTheyDeclare(t *testing.T) {
	api, _, f := setup(t)
	f.allow = true

	const (
		readPerm  = "billing:catalog"
		writePerm = "billing:price"
	)
	ran := map[string]int{}
	day0 := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	api.RegisterResource(httpx.Resource{
		Module: "billing", Entity: "plan",
		Read: readPerm, OperatorRead: true,
		Write: writePerm, OperatorWrite: true,
		Count: func(context.Context) (int64, error) {
			ran["Count"]++
			return 3, nil
		},
		CountFiltered: func(_ context.Context, _ crud.Where) (int64, error) {
			ran["CountFiltered"]++
			return 1, nil
		},
		CountDays: func(_ context.Context, _ string, _, _ time.Time) ([]crud.DayCount, error) {
			ran["CountDays"]++
			return []crud.DayCount{{Day: day0, Count: 1}}, nil
		},
		List: func(_ context.Context, _ crud.Query) ([]map[string]any, int64, error) {
			ran["List"]++
			return []map[string]any{{"code": "pro"}}, 1, nil
		},
		Get: func(_ context.Context, _ uuid.UUID) (map[string]any, error) {
			ran["Get"]++
			return map[string]any{"code": "pro"}, nil
		},
		Create: func(_ context.Context, _ map[string]any) (map[string]any, error) {
			ran["Create"]++
			return map[string]any{"code": "pro"}, nil
		},
		Update: func(_ context.Context, _ uuid.UUID, _ map[string]any) (map[string]any, error) {
			ran["Update"]++
			return map[string]any{"code": "pro"}, nil
		},
		Delete: func(_ context.Context, _ uuid.UUID) error {
			ran["Delete"]++
			return nil
		},
		History: &httpx.History{Versions: func(_ context.Context, _ uuid.UUID) ([]httpx.Version, error) {
			ran["History.Versions"]++
			return []httpx.Version{{}}, nil
		}},
	})
	door := httpx.Resource{}
	for _, r := range api.Resources() {
		if r.Module == "billing" && r.Entity == "plan" {
			door = r
		}
	}
	if door.Entity != "plan" {
		t.Fatal("the API recorded no plan resource: one that was never registered has no closures to ask")
	}

	// Each door names the permission its own declaration carries, which is how a
	// verb wrapped with the wrong one is caught: both declarations refuse this
	// caller, so only the refusal's own words say which guard stood in front.
	id := uuid.New()
	day := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	doors := []struct {
		name string
		perm string
		call func(context.Context) error
	}{
		{"Count", readPerm, func(ctx context.Context) error { _, err := door.Count(ctx); return err }},
		{"CountFiltered", readPerm, func(ctx context.Context) error { _, err := door.CountFiltered(ctx, crud.Where{}); return err }},
		{"CountDays", readPerm, func(ctx context.Context) error { _, err := door.CountDays(ctx, "created_at", day, day); return err }},
		{"List", readPerm, func(ctx context.Context) error { _, _, err := door.List(ctx, crud.Query{Limit: 10}); return err }},
		{"Get", readPerm, func(ctx context.Context) error { _, err := door.Get(ctx, id); return err }},
		{"History.Versions", readPerm, func(ctx context.Context) error {
			_, err := door.History.Versions(ctx, id)
			return err
		}},
		{"Create", writePerm, func(ctx context.Context) error { _, err := door.Create(ctx, map[string]any{"code": "pro"}); return err }},
		{"Update", writePerm, func(ctx context.Context) error {
			_, err := door.Update(ctx, id, map[string]any{"code": "pro"})
			return err
		}},
		{"Delete", writePerm, func(ctx context.Context) error { return door.Delete(ctx, id) }},
	}

	// The strongest caller a customer has: an administrator whose roles answer
	// every permission in their own tenant, which is why the boundary is asked
	// before the roles table is.
	in := func(tenant tenancy.Tenant) context.Context {
		ctx := tenancy.WithTenant(t.Context(), tenant)
		return tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: uuid.New(), Roles: []string{"admin"}})
	}
	customer := in(f.tenant)
	f.asked.Store(0)
	for _, d := range doors {
		err := d.call(customer)
		var refused *problem.Problem
		if !errors.As(err, &refused) {
			t.Errorf("%s of a resource declared for the operator answered a customer's administrator with %v: the door answers with a problem a client can show, and %d run(s) of its body means it opened", d.name, err, ran[d.name])
			continue
		}
		if refused.Status != http.StatusForbidden || !strings.Contains(refused.Detail, httpx.CodeNotOperator) || !strings.Contains(refused.Detail, d.perm) {
			t.Errorf("%s of a resource declared for the operator answered a customer's administrator with %d %q: the refusal this door owes names the operator boundary (%s) and the permission it declares (%s), which an outage, a bare denial or the other door's guard cannot", d.name, refused.Status, refused.Detail, httpx.CodeNotOperator, d.perm)
		}
	}
	for _, d := range doors {
		if ran[d.name] != 0 {
			t.Errorf("the refused %s ran its body %d time(s), want never: a generated form and a dashboard card call this closure, not the route", d.name, ran[d.name])
		}
	}
	if got := f.asked.Load(); got != 0 {
		t.Errorf("the refused doors asked the authorizer %d time(s), want never: the tenant flag decides all nine before the roles table, whose answer for a customer's administrator is always yes", got)
	}
	if door.Readable(customer) {
		t.Error("the resource declared itself readable to a customer's administrator: the catalogue, the navigation and every derived screen mount or refuse from this answer")
	}
	if door.Writable(customer) {
		t.Error("the resource declared itself writable to a customer's administrator: the same declaration the nine closures above answer from")
	}

	// The control: the same nine closures, the same mount, the tenant the
	// declaration names. Each asks the authorizer once, so the count also says
	// no door answered from a cache of its sibling's decision.
	f.tenant.Operator = true
	installation := in(f.tenant)
	if !door.Readable(installation) {
		t.Error("the resource declared itself unreadable to the installation whose read permission it carries")
	}
	if !door.Writable(installation) {
		t.Error("the resource declared itself not writable to the installation whose write permission it carries")
	}
	for k := range ran {
		delete(ran, k)
	}
	f.asked.Store(0)
	for _, d := range doors {
		if err := d.call(installation); err != nil {
			t.Errorf("%s of a resource declared for the operator refused the installation: %v", d.name, err)
		}
	}
	for _, d := range doors {
		if ran[d.name] != 1 {
			t.Errorf("the installation's %s ran its body %d time(s), want once", d.name, ran[d.name])
		}
	}
	if got, want := f.asked.Load(), int32(len(doors)); got != want {
		t.Errorf("the installation's nine calls asked the authorizer %d time(s), want %d: past the boundary each door asks its own grant", got, want)
	}
}
