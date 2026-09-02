package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
	"github.com/septagon-oss/platformkit/modules/user/internal"
)

var (
	acme        = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	globex      = tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	errRollback = errors.New("rolled back on purpose")
)

// TestServiceConforms runs the same suite the fake runs, against the real
// service, a real Postgres and a real tenant transaction.
func TestServiceConforms(t *testing.T) {
	usertest.RunService(t, func(t *testing.T, run func(usertest.Fixture)) {
		_, conn := dbtest.Schema(t)
		svc := internal.NewService()
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(usertest.Fixture{
				Ctx: ctx, Tx: tx, Service: svc,
				Published: func() []string { return outbox(t, tx) },
			})
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("the case's transaction: %v", err)
		}
	})
}

// outbox is what has been published in this transaction, in order.
func outbox(t *testing.T, tx db.Tx[db.Tenant]) []string {
	t.Helper()
	var names []string
	err := tx.DB().Table("platformkit_outbox").Order("created_at, id").Pluck("name", &names).Error
	if err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	return names
}

// TestOneAddressPerTenantAndNotOnePerInstallation is what "no memberships"
// means in practice: the same person working for two customers is two rows,
// each protected by its own tenant's policy, and neither can see the other.
func TestOneAddressPerTenantAndNotOnePerInstallation(t *testing.T) {
	_, conn := dbtest.Schema(t)
	svc := internal.NewService()

	ids := map[string]uuid.UUID{}
	for _, tenant := range []tenancy.Tenant{acme, globex} {
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			u, err := svc.Invite(ctx, tx, "ada@example.com", "Ada")
			if err != nil {
				return err
			}
			ids[tenant.Slug] = u.ID
			return nil
		})
		if err != nil {
			t.Fatalf("invite in %s: %v", tenant.Slug, err)
		}
	}
	if ids["acme"] == ids["globex"] {
		t.Fatal("the same address in two tenants produced one row")
	}

	// Acme's transaction sees Acme's Ada and nothing else, and cannot read
	// Globex's by asking for it by id.
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		mine, err := svc.ByEmail(ctx, tx, "ada@example.com")
		if err != nil {
			return err
		}
		if mine.ID != ids["acme"] {
			t.Errorf("ByEmail returned %s, want this tenant's row", mine.ID)
		}
		if _, err := svc.Get(ctx, tx, ids["globex"]); err == nil {
			t.Error("one tenant read another tenant's user by id")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read as acme: %v", err)
	}
}

// TestAPasswordIsNeverStoredAndTheHashCarriesItsParameters: the encoding is
// what makes today's argon2id parameters raisable tomorrow without invalidating
// anybody's password, so it is pinned here rather than assumed.
func TestAPasswordIsNeverStoredAndTheHashCarriesItsParameters(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	svc := internal.NewService()
	const password = "correct horse battery staple"

	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := svc.Invite(ctx, tx, "ada@example.com", "Ada")
		if err != nil {
			return err
		}
		id = u.ID
		return svc.SetPassword(ctx, tx, u.ID, password)
	})
	if err != nil {
		t.Fatalf("set a password: %v", err)
	}

	var stored string
	if err := admin.QueryRowContext(t.Context(), `SELECT password_hash FROM users WHERE id = $1`, id).Scan(&stored); err != nil {
		t.Fatalf("read the hash: %v", err)
	}
	const want = "$argon2id$v=19$m=65536,t=1,p=4$"
	if len(stored) < len(want) || stored[:len(want)] != want {
		t.Errorf("the stored hash is %q; the parameters travel with it so that raising them later leaves old hashes verifiable", stored)
	}
	u := &contracts.User{PasswordHash: stored}
	if !u.CheckPassword(password) || u.CheckPassword(password+" ") {
		t.Error("the stored hash does not verify what it was made from, or verifies something else")
	}
}

// TestProvisionIsTheBootstrapsDoorAndNobodyElses: the first administrator of an
// installation is created in the same transaction as the tenant they
// administer, which belongs to no tenant.
func TestProvisionIsTheBootstrapsDoorAndNobodyElses(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	svc := internal.NewService()

	var id uuid.UUID
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		u, err := svc.Provision(ctx, tx, acme.ID, "root@example.com", "Root",
			"correct horse battery staple", []string{"admin"})
		if err != nil {
			return err
		}
		id = u.ID
		if !u.CanSignIn() {
			t.Error("a provisioned administrator cannot sign in")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	// The row lands in the named tenant, and the event that says so carries
	// that tenant too: events.PublishFor is the only place a tenant is an
	// argument, and this is what it is for.
	var tenantID uuid.UUID
	if err := admin.QueryRowContext(t.Context(), `SELECT tenant_id FROM users WHERE id = $1`, id).Scan(&tenantID); err != nil {
		t.Fatalf("read the user: %v", err)
	}
	if tenantID != acme.ID {
		t.Errorf("the provisioned user belongs to %s, want %s", tenantID, acme.ID)
	}
	var events int
	err = admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE name = $1 AND tenant_id = $2`,
		contracts.EventInvited, acme.ID).Scan(&events)
	if err != nil {
		t.Fatalf("count the events: %v", err)
	}
	if events != 1 {
		t.Errorf("Provision published %d events for the tenant, want one", events)
	}
}
