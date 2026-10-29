package httpx_test

// the_narrowed_totals_a_registration_supplies_refuse_a_caller_it_cannot_place_test.go
// asks what the narrowed totals answer to a caller the installation cannot place.
//
// Registration wraps each reading closure in the guard the resource declares, and
// the guard speaks twice before it asks the Authorizer anything: a request whose
// host resolved to no tenant is tenant work with nobody's rows to count, and a
// request nobody recognises is anonymous. Those two refusals are all that stands
// between a signed-out caller and the totals a tenant's cards read. The total a
// list carries is pinned on both doors that write it, and every narrowed total is
// pinned against a caller the roles table turns away — a caller who was placed, so
// the question was asked. Nobody had asked the two callers standing in front of
// that question of the two narrowed totals: a count narrowed by a filter and a
// count per day are separate wrappings in the same guard, each one droppable on its
// own while every other door still refuses. So this case asks both totals, and the
// list behind them, as a tenant with nobody recognised and as no tenant at all, and
// owes each the refusal that fits with nothing run and no grant consulted; the same
// doors, asked by a caller the installation can place, answer, which is what keeps
// the refusals from being doors that read nothing anyway.

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

// TestTheNarrowedTotalsARegistrationSuppliesRefuseACallerItCannotPlace registers a
// resource that declares an ordinary read permission and writes a list, a count
// narrowed by a filter and a count per day, then asks all three as a tenant with no
// recognised caller, which owes 401 AUTH_ANONYMOUS, and as no tenant at all, which
// owes 403 AUTH_NO_TENANT. Each refusal owes a zero answer, no run of the closure
// behind it and no question put to the Authorizer, because a caller nobody placed has
// no grant to be asked about. The same three doors, asked by a caller the
// installation recognises while the Authorizer grants, each answer once.
func TestTheNarrowedTotalsARegistrationSuppliesRefuseACallerItCannotPlace(t *testing.T) {
	api, _, f := setup(t)

	const readPerm = "billing:catalog"
	day := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	ran := map[string]int{}
	// The authority allows everything it is asked about, so the only refusal a
	// caller can meet here is the one the door's own placement check answers.
	f.allow = true
	api.RegisterResource(httpx.Resource{
		Module: "billing", Entity: "plan", Read: readPerm,
		List: func(context.Context, crud.Query) ([]map[string]any, int64, error) {
			ran["List"]++
			return []map[string]any{{"code": "pro"}}, 7, nil
		},
		CountFiltered: func(context.Context, crud.Where) (int64, error) {
			ran["CountFiltered"]++
			return 3, nil
		},
		CountDays: func(context.Context, string, time.Time, time.Time) ([]crud.DayCount, error) {
			ran["CountDays"]++
			return []crud.DayCount{{Day: day, Count: 1}}, nil
		},
	})
	door := httpx.Resource{}
	for _, r := range api.Resources() {
		if r.Module == "billing" && r.Entity == "plan" {
			door = r
		}
	}
	if door.CountFiltered == nil || door.CountDays == nil {
		t.Fatal("registration supplied no narrowed total for a resource that writes them: a card that filters this collection has no number to show")
	}

	doors := []struct {
		name string
		call func(context.Context) (int64, int, error)
	}{
		{"CountFiltered", func(ctx context.Context) (int64, int, error) {
			total, err := door.CountFiltered(ctx, crud.Where{})
			return total, 0, err
		}},
		{"CountDays", func(ctx context.Context) (int64, int, error) {
			days, err := door.CountDays(ctx, "created_at", day, day)
			return 0, len(days), err
		}},
		{"List", func(ctx context.Context) (int64, int, error) {
			rows, total, err := door.List(ctx, crud.Query{Limit: 10})
			return total, len(rows), err
		}},
	}

	for _, arm := range []struct {
		who    string
		ctx    context.Context
		status int
		code   string
	}{
		// A tenant with no recognised caller: the door cannot tell who is reading,
		// so it sends the caller to sign in rather than count anybody's rows.
		{"a tenant with nobody recognised", tenancy.WithTenant(t.Context(), f.tenant),
			http.StatusUnauthorized, httpx.CodeAnonymous},
		// No tenant at all: the host resolved to none, so this is tenant work with
		// nobody's rows to count, and no sign-in would change that.
		{"a host that resolves to no tenant", t.Context(),
			http.StatusForbidden, httpx.CodeNoTenant},
	} {
		for _, d := range doors {
			for name := range ran {
				ran[name] = 0
			}
			f.asked.Store(0)
			total, rows, err := d.call(arm.ctx)
			var refused *problem.Problem
			if !errors.As(err, &refused) {
				t.Errorf("%s answered %s with %d rows, %d and %v: the guard in front of this closure owes the caller the refusal that fits",
					d.name, arm.who, rows, total, err)
				continue
			}
			if refused.Status != arm.status || !strings.Contains(refused.Detail, arm.code) {
				t.Errorf("%s answered %s with %d %q, want %d naming %s: the refusal has to say which of the two it is, because signing in answers one and not the other",
					d.name, arm.who, refused.Status, refused.Detail, arm.status, arm.code)
			}
			if total != 0 || rows != 0 {
				t.Errorf("%s answered %d and %d rows beside its %d, want neither: a narrowed total with no error is a card full of a tenant's rows, and a caller who was refused has no business reading any",
					d.name, total, rows, refused.Status)
			}
			if ran[d.name] != 0 {
				t.Errorf("%s ran its own closure %d time(s) for %s it refused, want not at all: the count is the work here, and a guard that arrives after it spent the work it refuses",
					d.name, ran[d.name], arm.who)
			}
			if got := f.asked.Load(); got != 0 {
				t.Errorf("%s asked the authorizer %d time(s) for %s, want never: nobody was placed, so no grant was decided",
					d.name, got, arm.who)
			}
		}
	}

	// The control: the same doors, the same grant, a caller the installation can
	// place. Without this the refusals above would only have shown doors that read
	// nothing.
	caller := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), f.tenant),
		tenancy.Principal{UserID: uuid.New(), Roles: []string{"member"}})
	for _, d := range doors {
		for name := range ran {
			ran[name] = 0
		}
		f.asked.Store(0)
		if _, _, err := d.call(caller); err != nil {
			t.Errorf("%s refused a caller it can place who holds %q: %v", d.name, readPerm, err)
			continue
		}
		if ran[d.name] != 1 {
			t.Errorf("%s ran its own closure %d time(s) for a caller it answered, want once", d.name, ran[d.name])
		}
		if got := f.asked.Load(); got != 1 {
			t.Errorf("%s asked the authorizer %d time(s) for a caller it answered, want once: this declaration names a permission, so the answer has to come from the thing that decides permissions",
				d.name, got)
		}
	}
}
