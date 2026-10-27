package rest_test

// the_total_a_singleton_is_supplied_asks_the_authorizer_about_the_tenant_that_asked_test.go
// asks which tenant a singleton's total is permitted for.
//
// httpx.Resource.Count is a door a page, a catalogue entry and an in-process
// caller all reach, and the guard registration writes for it asks the Authorizer
// about the tenant standing in the caller's context. A guard that decided the
// grant for the tenant the host resolves to, while reading rows under the
// transaction of the tenant that asked, would answer every caller who happens to
// hold the host's grant and refuse the rest. So the authority here answers by
// tenant: one of two tenants holds "task:read" and the other does not, and both
// hold a row. Each asks the same mounted door from inside its own transaction,
// and the case asks three things of each answer — the number is the rows that
// transaction may count, the row behind it is the asker's own, and the permission
// was decided for the asker and for nobody else. The tenant the host resolves to
// holds nothing: refused 403, no total, nothing read, one decision naming itself.

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

// tenantKeyedAuthority answers the permission question differently for each
// tenant it is asked about, and remembers every tenant it was asked about, in
// the order it was asked. ByHost resolves to holder, so a guard that reached for
// the host's tenant rather than the caller's can be told apart from one that
// asks about the caller.
type tenantKeyedAuthority struct {
	holder  tenancy.Tenant
	holders map[uuid.UUID][]string
	asked   []uuid.UUID
}

func (f *tenantKeyedAuthority) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return f.holder, nil
}

func (f *tenantKeyedAuthority) Allowed(_ context.Context, tenant tenancy.Tenant, grant tenancy.Grant) (bool, error) {
	f.asked = append(f.asked, tenant.ID)
	for _, held := range f.holders[tenant.ID] {
		if held == grant.Permission {
			return true, nil
		}
	}
	return false, nil
}

// TestTheSuppliedTotalOfASingletonAsksTheAuthorizerAboutTheTenantThatAsked mounts
// a singleton over the package's tenant-guarded table, seeds one row for each of
// two tenants, grants "task:read" to one of them, and asks the total registration
// supplies as each from inside that tenant's own transaction: the holder is
// answered with the one row its transaction can count, its own row behind the
// number, and one permission decision naming its own tenant; the caller that does
// not hold it is refused 403 with no total, nothing read, and one decision naming
// its own tenant rather than the host's.
func TestTheSuppliedTotalOfASingletonAsksTheAuthorizerAboutTheTenantThatAsked(t *testing.T) {
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
		t.Fatalf("create the singleton's table: %v", err)
	}
	// acme is what the host resolves to and holds nothing; globex holds the read
	// permission while resolving no host at all. Which of the two is answered
	// therefore cannot follow the host, and the two rows say whose read it was.
	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex"}
	const globexTitle = "Globex's own board"
	seeds := []struct {
		tenant tenancy.Tenant
		title  string
	}{
		{acme, "Acme's own board"},
		{globex, globexTitle},
	}
	for _, seed := range seeds {
		if _, err := admin.ExecContext(t.Context(),
			"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)",
			uuid.New(), seed.tenant.ID, seed.title); err != nil {
			t.Fatalf("seed %s's row: %v", seed.tenant.Slug, err)
		}
	}
	authority := &tenantKeyedAuthority{holder: acme, holders: map[uuid.UUID][]string{globex.ID: {"task:read"}}}
	api, _ := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: authority, Conn: app,
		Authorize: authority, Installation: host,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	var (
		loads int64
		seen  int64
		own   string
	)
	shelf := rest.Singleton[*Settings]{
		Module: "site", Entity: "settings", Path: "/settings",
		Read: "task:read", Write: "task:write",
		Save: func(_ context.Context, _ db.Tx[db.Tenant], in *Settings) (*Settings, error) {
			return in, nil // never reached: this case only ever asks the total.
		},
		// The read a module writes for a singleton of this kind, and the count of
		// what its own transaction is allowed to see — the number the total answers.
		Load: func(_ context.Context, tx db.Tx[db.Tenant]) (*Settings, error) {
			var out Settings
			if err := tx.DB().Where("deleted_at IS NULL").Take(&out).Error; err != nil {
				return nil, err
			}
			seen = 0
			if err := tx.DB().Model(&Settings{}).Where("deleted_at IS NULL").Count(&seen).Error; err != nil {
				return nil, err
			}
			loads++
			own = db.TenantOf(tx).Slug + ":" + out.Title
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

	// Ask the door as one tenant, inside that tenant's own transaction, and say
	// what the authorizer was asked about on the way in.
	askTotal := func(tenant tenancy.Tenant) (int64, error, []uuid.UUID, int64) {
		askedBefore := len(authority.asked)
		loadsBefore := loads
		var total int64
		var err error
		ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), tenant),
			tenancy.Principal{UserID: principal, Roles: []string{"member"}})
		if e := db.Run(ctx, app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
			total, err = door.Count(ctx)
			return nil
		}); e != nil {
			t.Fatalf("%s's total opened no transaction: %v", tenant.Slug, e)
		}
		return total, err, authority.asked[askedBefore:], loads - loadsBefore
	}

	// The tenant that holds the permission, though the host resolves to the other.
	total, err, asked, reads := askTotal(globex)
	if err != nil {
		t.Fatalf("the singleton's supplied total refused %s, who holds %q and was refused nothing: %v", globex.Slug, "task:read", err)
	}
	if total != 1 || seen != 1 {
		t.Errorf("the singleton's supplied total answered %d for %s, want %d: that is how many rows this caller's transaction can count, and the badge beside this collection shows whatever this returns", total, globex.Slug, seen)
	}
	if own != globex.Slug+":"+globexTitle {
		t.Errorf("the singleton's total for %s was read from %q, want that tenant's own row", globex.Slug, own)
	}
	if reads != 1 {
		t.Errorf("the singleton's total for %s read its row %d times in that one call, want once", globex.Slug, reads)
	}
	if len(asked) != 1 || asked[0] != globex.ID {
		t.Errorf("the singleton's total for %s had its grant decided for %v, want exactly one decision naming %s: a total permitted for some tenant other than the caller is a total decided for somebody else", globex.Slug, asked, globex.ID)
	}

	// The tenant the host resolves to, which holds nothing: the same door refuses
	// it, names it in the denial, and reads nothing on the way.
	authority.asked = nil
	total, err, asked, reads = askTotal(acme)
	if err == nil {
		t.Fatalf("the singleton's supplied total answered %d to %s, who does not hold %q, want a refusal", total, acme.Slug, "task:read")
	}
	if refused := (*problem.Problem)(nil); !errors.As(err, &refused) || refused.Status != http.StatusForbidden {
		t.Errorf("the singleton's supplied total answered %s's missing grant with %v, want the 403 that says so", acme.Slug, err)
	}
	if total != 0 {
		t.Errorf("the singleton's supplied total answered %d beside %s's refusal, want no total", total, acme.Slug)
	}
	if reads != 0 {
		t.Errorf("the singleton's supplied total read the module's row %d times for %s, who holds no read permission, want not at all", reads, acme.Slug)
	}
	if len(asked) != 1 || asked[0] != acme.ID {
		t.Errorf("the refused total for %s had its grant decided for %v, want exactly one decision naming %s and not the host's tenant: %s resolves to %s, so this ask cannot tell the two apart by accident", acme.Slug, asked, acme.ID, host, acme.Slug)
	}
}
