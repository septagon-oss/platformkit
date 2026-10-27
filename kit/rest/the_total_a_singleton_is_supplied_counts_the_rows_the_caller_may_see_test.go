package rest_test

// the_total_a_singleton_is_supplied_counts_the_rows_the_caller_may_see_test.go
// asks the only total a singleton has on behalf of the callers around it.
//
// rest.Singleton hands registration a resource that names no Count of its own,
// so the number beside a singleton's collection — the badge the admin workspace
// draws — is the one registration supplies, read through the singleton's List,
// which is its Load. Two things have to hold of that number at once, and only a
// singleton ties them together: it is one, because a tenant has exactly one of
// these rows, and it counts the rows the *asking* tenant may see, because the
// read runs through the caller's transaction and row-level security shows a
// tenant transaction only its own rows. A total read outside that transaction
// cannot answer both — with no transaction it has nothing to answer with, and
// with the wrong one it counts rows this caller was never allowed to see. So
// each of two tenants asks the same mounted door from inside its own
// transaction, and the number is checked against the rows that same transaction
// can count and the row against its own title.
//
// The third ask comes from a caller who does not hold the read permission. A
// singleton's total is the one total a module may compute itself rather than
// count, so this is the door where "asked, then read" and "read, then asked"
// differ: the refusal is the same either way, and only the read tells them apart.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
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

// TestTheSuppliedTotalOfASingletonCountsTheRowsTheAskingTenantMaySee mounts a
// singleton over the package's tenant-guarded table, seeds one row for each of
// two tenants, and asks the total registration supplies as each of them from
// inside that tenant's own transaction: the number of rows that transaction can
// count, no error, one read, one grant ask, and the asking tenant's own row
// behind it. A third ask from a caller without the read permission is refused,
// answers no total and reads nothing at all.
func TestTheSuppliedTotalOfASingletonCountsTheRowsTheAskingTenantMaySee(t *testing.T) {
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
		t.Fatalf("create the singleton's table: %v", err)
	}
	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex"}
	// One row per tenant, written from the system transaction. The title is the
	// row's own name for who it belongs to, so a read that reached the wrong
	// tenant says so in the row rather than in this case's bookkeeping.
	seeds := []struct {
		tenant tenancy.Tenant
		title  string
	}{
		{acme, "Acme's own board"},
		{globex, "Globex's own board"},
	}
	for _, seed := range seeds {
		if _, err := admin.ExecContext(t.Context(),
			"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)",
			uuid.New(), seed.tenant.ID, seed.title); err != nil {
			t.Fatalf("seed %s's row: %v", seed.tenant.Slug, err)
		}
	}
	ask := &operatorReader{tenant: acme, allow: true}
	api, _ := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: ask, Conn: app,
		Authorize: ask, Installation: host,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	var (
		loads   int      // reads of the module's row, in the order they ran
		seen    int64    // rows the last read's own transaction could count
		reached []string // the row each read reached, as "<tenant it ran for>:<title>"
		before  int      // reads counted before the ask being answered now
	)
	shelf := rest.Singleton[*Settings]{
		Module: "site", Entity: "settings", Path: "/settings",
		Read: "task:read", Write: "task:write",
		Save: func(_ context.Context, _ db.Tx[db.Tenant], in *Settings) (*Settings, error) {
			return in, nil // never reached: this case only ever asks the total.
		},
		// The read a module writes for a singleton of this kind: its own
		// transaction, the row it holds, and a count of what that transaction is
		// allowed to see — which is the number the total has to answer with.
		Load: func(_ context.Context, tx db.Tx[db.Tenant]) (*Settings, error) {
			var out Settings
			if err := tx.DB().Where("deleted_at IS NULL").Take(&out).Error; err != nil {
				return nil, err
			}
			var visible int64
			if err := tx.DB().Model(&Settings{}).Where("deleted_at IS NULL").Count(&visible).Error; err != nil {
				return nil, err
			}
			loads++
			seen, reached = visible, append(reached, db.TenantOf(tx).Slug+":"+out.Title)
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
	// Registration supplies the total through the Load. If that ever stops, the
	// card beside this collection has no number to show, and this is where it
	// says so rather than in whatever reads the nil.
	if door.Count == nil {
		t.Fatal("registration supplied no total for a mounted singleton: the card that reads this collection has no number to show")
	}

	// The caller holds the grant throughout the two asks that follow, so anything
	// refused there is the read failing and not the caller failing.
	held := func(tenant tenancy.Tenant) context.Context {
		return tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), tenant),
			tenancy.Principal{UserID: principal, Roles: []string{"member"}})
	}
	askTotal := func(ctx context.Context) (int64, error) {
		ask.asked, seen, before = 0, 0, loads
		var total int64
		var err error
		if e := db.Run(ctx, app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
			total, err = door.Count(ctx)
			return nil
		}); e != nil {
			t.Fatalf("the total's door opened no transaction: %v", e)
		}
		return total, err
	}

	for _, want := range seeds {
		total, err := askTotal(held(want.tenant))
		if err != nil {
			t.Fatalf("the singleton's supplied total refused %s, who holds the grant and was refused nothing: %v", want.tenant.Slug, err)
		}
		if loads != before+1 {
			t.Fatalf("the singleton's total for %s read its row %d times in that one call, want once", want.tenant.Slug, loads-before)
		}
		if seen != 1 {
			t.Fatalf("the case seeded %s wrong: its own transaction counted %d rows, want one", want.tenant.Slug, seen)
		}
		if total != seen {
			t.Errorf("the singleton's supplied total answered %d for %s, want %d: that is how many rows this caller's transaction can count, and the badge beside this collection shows whatever this returns", total, want.tenant.Slug, seen)
		}
		// And the row that total was read from is the caller's own, not whichever
		// row a read that ignored the caller's transaction happened to reach.
		if own := want.tenant.Slug + ":" + want.title; reached[loads-1] != own {
			t.Errorf("the singleton's total for %s was read from %q, want that tenant's own row %q: a total read outside the caller's transaction counts rows this caller may not see", want.tenant.Slug, reached[loads-1], own)
		}
		if ask.asked != 1 {
			t.Errorf("the singleton's total for %s asked the authorizer %d times, want once: the caller held the grant, and a total is one check of who is asking", want.tenant.Slug, ask.asked)
		}
	}
	if loads != 2 {
		t.Errorf("the two tenants' totals read the singleton %d times in all, want twice: each total is one read", loads)
	}

	// The same door, to a caller who does not hold the read permission: refused,
	// no total, and the module's Load untouched beside the refusal.
	ask.allow = false
	total, err := askTotal(held(acme))
	if err == nil {
		t.Fatalf("the singleton's supplied total answered %d to a caller who does not hold %q, want a refusal", total, "task:read")
	}
	if refused := (*problem.Problem)(nil); !errors.As(err, &refused) || refused.Status != http.StatusForbidden {
		t.Errorf("the singleton's supplied total answered its caller's missing grant with %v, want the 403 that says so", err)
	}
	if total != 0 {
		t.Errorf("the singleton's supplied total answered %d beside the refusal, want no total: the number came out of a door this caller may not open", total)
	}
	if loads != before {
		t.Errorf("the singleton's supplied total read the module's row %d times for a caller who holds no read permission, want not at all", loads-before)
	}
	if ask.asked != 1 {
		t.Errorf("the refused total asked the authorizer %d times, want once", ask.asked)
	}
}
