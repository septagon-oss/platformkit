package internal_test

// Review round 1 of T-0115, at the module: `Delete` releases the slug and traps the
// host.
//
// `contracts/tenant.go`'s own comment on `Delete` — the file every other module reads —
// says the write releases the name "so the name can be given to a new customer later",
// and `internal/service.go` repeats it ("the same name can be handed to a new customer
// later"). The routing key of this platform is not the slug: it is `tenant_hosts.host`,
// whose PRIMARY KEY (migrations/000006) is global. `Delete` writes one column and
// touches no host row, so the retired tenant keeps every name it answered at, and from
// then on nothing in the module can move them: `RemoveHost` reaches a tenant through
// `lock`, which filters `deleted_at IS NULL` and answers not-found, and `AddHost` for
// that name is a primary-key violation on a row no read can find. The hostname is
// reserved forever by a tenant nobody can see.
//
// The two writes below are one pair, and the first is the reachability control: the
// freed *slug* really is reusable, at another host, today. So this file reaches its
// assertion through what the fixed behaviour prints — a created tenant, and the row its
// host resolves to — never through anything a broken answer would print.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/trace"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestARetiredTenantsHostIsServableAgain(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	svc := internal.NewService(nil, nil)
	ctx := trace.With(t.Context(), trace.New())

	var acme uuid.UUID
	fail := func(what string, err error) {
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}

	fail("install an installation and one customer", dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if _, err := internal.Bootstrap(ctx, tx, svc, contracts.NewTenant{
			Slug: "installation", Name: "This installation", Host: "ops.example.com",
		}); err != nil {
			return err
		}
		created, err := svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"})
		if err != nil {
			return err
		}
		acme = created.ID
		_, err = svc.Delete(ctx, tx, acme, contracts.Delete{Confirm: "acme"})
		return err
	}))

	// The control: the retired tenant's slug is free, exactly as the contract says,
	// for a customer that arrives at a name nobody holds.
	fail("host a new tenant at a free name under the freed slug", dbtest.System(ctx, conn,
		func(ctx context.Context, tx db.Tx[db.System]) error {
			_, err := svc.Create(ctx, tx, contracts.NewTenant{
				Slug: "acme", Name: "Acme Renewed", Host: "acme-renewed.example.com",
			})
			return err
		}))

	// The case: the retired tenant's own host, which no live tenant answers at.
	var successor uuid.UUID
	err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		created, err := svc.Create(ctx, tx, contracts.NewTenant{
			Slug: "second", Name: "Second Customer", Host: "acme.example.com",
		})
		if err != nil {
			return err
		}
		successor = created.ID
		resolved, err := svc.ByHost(ctx, tx, "acme.example.com")
		if err != nil {
			return err
		}
		if resolved.ID != successor {
			t.Errorf("acme.example.com resolves to %s, want the tenant just hosted there (%s)", resolved.ID, successor)
		}
		return nil
	})
	if err != nil {
		t.Errorf("a hostname retired with a tenant is servable again: acme.example.com is not — %v. "+
			"The retired row still holds the tenant_hosts primary key, `RemoveHost` cannot reach a deleted "+
			"tenant to free it, and `AddHost` cannot take it: the name is reserved forever.", err)
	}

	// And the retired customer's own row is still there: the history a delete is
	// supposed to keep, asserted here so the fix above cannot be bought by erasing it.
	var retired int
	fail("count the retired tenant", dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Table("tenants").Select("count(*)").
			Where("id = ? AND deleted_at IS NOT NULL", acme).Scan(&retired).Error
	}))
	if retired != 1 {
		t.Errorf("the retired customer's row is gone (%d rows found), want it kept with deleted_at set", retired)
	}
}
