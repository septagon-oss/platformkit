package rest_test

// the_total_a_singleton_is_supplied_answers_no_number_to_a_caller_it_cannot_place_test.go
// asks what a singleton's total says to a caller it cannot place.
//
// The total registration supplies for a singleton is the number an admin card, a
// catalogue entry and an in-process caller all read, and the guard written for it
// refuses in three steps before it asks the Authorizer anything: a request that
// resolved to no tenant is tenant work with no tenant, a request nobody recognises
// is anonymous, and only then comes the grant. Those two first refusals are all that
// stands between a signed-out caller and a count of a tenant's rows, and every case
// beside this one arrives with a session: the others on this door ask whose grant
// decided the number, whose rows it counted and what a failed read answers with. So
// this case arrives with nobody — as a tenant with no recognised caller, and with no
// tenant at all. Each owes the caller the refusal that fits, no number, no read of
// the module's row and no grant consulted; and the same door, asked by a caller it
// can place, is answered with a number, so the two refusals are the guard speaking
// rather than a door that reads nothing anyway.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestTheSuppliedTotalOfASingletonAnswersNoNumberToACallerItCannotPlace mounts a
// singleton over the package's tenant-guarded table, seeds one row, and asks the
// total registration supplies three times: as a tenant with no recognised caller,
// which owes 401 AUTH_ANONYMOUS; with no tenant in the context at all, which owes
// 403 AUTH_NO_TENANT; and as a caller the installation recognises, which is answered
// with the one row its tenant holds. Each refusal owes no total, no read of the
// module's row and no question put to the Authorizer, because a caller nobody placed
// has no grant to be asked about.
func TestTheSuppliedTotalOfASingletonAnswersNoNumberToACallerItCannotPlace(t *testing.T) {
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
		t.Fatalf("create the singleton's table: %v", err)
	}
	// One row exists, so a door that skipped its guard would answer a number rather
	// than fail for a reason of its own: that number is what such a door leaks.
	if _, err := admin.ExecContext(t.Context(),
		"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)",
		uuid.New(), acme.ID, "Acme's own board"); err != nil {
		t.Fatalf("seed the singleton's row: %v", err)
	}
	// The authority allows everything it is asked about, so the only refusal a
	// caller can meet here is the one the door's own placement check answers.
	gate := &operatorReader{tenant: acme, allow: true}
	api, _ := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: gate, Conn: app,
		Authorize: gate, Installation: host,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	var loads int
	shelf := rest.Singleton[*Settings]{
		Module: "site", Entity: "settings", Path: "/settings",
		Read: "task:read", Write: "task:write",
		Save: func(_ context.Context, _ db.Tx[db.Tenant], in *Settings) (*Settings, error) {
			return in, nil // never reached: this case only ever asks the total.
		},
		Load: func(_ context.Context, tx db.Tx[db.Tenant]) (*Settings, error) {
			var out Settings
			if err := tx.DB().Where("deleted_at IS NULL").Take(&out).Error; err != nil {
				return nil, err
			}
			loads++
			return &out, nil
		},
	}
	shelf.Mount(api.Surfaces(shelf.Module))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted singleton does not declare itself: %v", err)
	}
	var door httpx.Resource
	for _, r := range api.Resources() {
		if r.Module == "site" && r.Entity == "settings" {
			door = r
		}
	}
	if door.Count == nil {
		t.Fatal("registration supplied no total for a mounted singleton: the card that reads this collection has no number to show")
	}

	// knock asks the door as the caller standing in ctx and reports what it did on
	// the way to its answer. A caller with a tenant is asked inside that tenant's own
	// transaction, as a card asks it; a caller with none is asked as it arrives, since
	// an open transaction is what a tenant makes possible.
	knock := func(ctx context.Context) (total int64, err error, grantAsks, reads int) {
		before, loadsBefore := gate.asked, loads
		if _, hasTenant := tenancy.FromContext(ctx); hasTenant {
			if e := db.Run(ctx, app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
				total, err = door.Count(ctx)
				return nil
			}); e != nil {
				t.Fatalf("the total's door opened no transaction: %v", e)
			}
		} else {
			total, err = door.Count(ctx)
		}
		return total, err, gate.asked - before, loads - loadsBefore
	}
	// refused checks the four things one refusal owes: its own status and reason, no
	// number, nothing read and nobody's grant consulted.
	refused := func(t *testing.T, total int64, err error, grantAsks, reads int, status int, code string) {
		t.Helper()
		var got *problem.Problem
		if !errors.As(err, &got) {
			t.Fatalf("the singleton's supplied total answered a caller it cannot place with %v, want the %d that says so", err, status)
		}
		if got.Status != status || !strings.Contains(got.Detail, code) {
			t.Errorf("the singleton's supplied total answered a caller it cannot place with %d %q, want %d naming %s: the refusal has to say which of the two it is, because signing in answers one and not the other", got.Status, got.Detail, status, code)
		}
		if total != 0 {
			t.Errorf("the singleton's supplied total answered %d beside its %d, want no number: a total with no error is a collection of that many rows, and a caller who was refused has no business reading one", total, status)
		}
		if reads != 0 {
			t.Errorf("the singleton's supplied total read the module's row %d times for a caller it refused with %d, want not at all", reads, status)
		}
		if grantAsks != 0 {
			t.Errorf("the singleton's supplied total asked the authorizer %d times for a caller it refused with %d, want never: nobody was placed, so no grant was decided", grantAsks, status)
		}
	}

	// A tenant with no recognised caller: the door cannot tell who is reading, so it
	// sends the caller to sign in rather than count anybody's rows.
	total, err, grantAsks, reads := knock(tenancy.WithTenant(t.Context(), acme))
	refused(t, total, err, grantAsks, reads, http.StatusUnauthorized, httpx.CodeAnonymous)

	// No tenant at all: the host resolved to none, so this is tenant work with
	// nobody's rows to count, and no sign-in would change that.
	total, err, grantAsks, reads = knock(t.Context())
	refused(t, total, err, grantAsks, reads, http.StatusForbidden, httpx.CodeNoTenant)

	// The control: the same door, the same grant and the same row, answered to a
	// caller it can place. Without this the two refusals above would only have shown
	// a total that never reads anything.
	total, err, grantAsks, reads = knock(tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), acme),
		tenancy.Principal{UserID: principal, Roles: []string{"member"}}))
	if err != nil {
		t.Fatalf("the singleton's supplied total refused a caller it can place who holds %q: %v", "task:read", err)
	}
	if total != 1 {
		t.Errorf("the singleton's supplied total answered %d for %s, want the one row it holds", total, acme.Slug)
	}
	if reads != 1 || grantAsks != 1 {
		t.Errorf("the singleton's supplied total read %d rows and asked the authorizer %d times, want one read and one ask", reads, grantAsks)
	}
}
