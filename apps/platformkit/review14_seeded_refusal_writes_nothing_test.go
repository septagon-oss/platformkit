package main

// Review 14's pin — brief §2: "a refusal at boot, not a warning … so the state the
// hourly warning describes cannot be created", and the house rule that a refused
// mutation writes nothing and emits nothing.
//
// Nothing committed holds this at the composition. `modules/auth/seed_test.go`
// refuses the same literal and counts *role* rows; `modules/tenant/internal`
// proves a failing hook rolls back the tenant it created. Nobody ran the two
// together: a hook whose error was swallowed after the tenant row and the
// `tenant.created` outbox row were written would leave an installation with a
// tenant and no roles at all — nobody inside it able to administer it — and both
// committed cases would stay green. So this asserts the refusal, the sentence,
// the tenant row, the outbox count and the tenant that already existed, reached
// through the row's own slug and the event table's own count rather than through
// anything a broken build would print.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

func TestACompositionNamingAnUndeclaredPermissionCreatesNoTenantAndEmitsNothing(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	count := func(what string) int {
		var n int
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Raw("SELECT count(*) FROM " + what).Scan(&n).Error
		})
		if err != nil {
			t.Fatalf("count %s: %v", what, err)
		}
		return n
	}
	events := count("platformkit_outbox")

	saved := initialRoles
	t.Cleanup(func() { initialRoles = saved })
	initialRoles = []authcontracts.Role{
		{Name: "clerk", Grants: authcontracts.Permissions{"task:read", "ghost:read"}},
	}
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		_, e := c.tenants.Create(ctx, tx, tenantcontracts.NewTenant{
			Slug: "violate", Name: "Violate", Host: "violate.localhost",
		})
		return e
	})
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("creating a tenant of a composition naming an undeclared permission: %v, want crud.ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), "clerk") || !strings.Contains(err.Error(), "ghost:read") {
		t.Errorf("the refusal names neither the role nor the permission: %v", err)
	}
	if n := count("tenants WHERE slug = 'violate'"); n != 0 {
		t.Errorf("the refused tenant's row committed: %d rows for slug violate", n)
	}
	if n := count("platformkit_outbox"); n != events {
		t.Errorf("the refused tenant created %d outbox rows over the %d already there", n, events)
	}

	// The refusal is the one mutation this case asked for; the tenant that was
	// already here keeps every grant its bootstrap seeded.
	var acme tenancy.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var e error
		acme, e = c.tenants.ByHost(ctx, tx, acmeHost)
		return e
	}); err != nil {
		t.Fatalf("read the bootstrapped tenant: %v", err)
	}
	admin := rolesOf(t, c, conn, acme)[authcontracts.RoleAdmin]
	for _, live := range []string{"*", "tenant:manage", "billing:catalog"} {
		if !slices.Contains(admin, live) {
			t.Errorf("a refused tenant creation took %q out of acme's administrator: %v", live, admin)
		}
	}
}
