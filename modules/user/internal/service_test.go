package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
	"github.com/septagon-oss/platformkit/modules/user/internal"
)

var (
	acme        = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	globex      = tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	errRollback = errors.New("rolled back on purpose")
)

// administering is this package's role system: one name, the same one the
// conformance suite and the fake use. The real answer is the auth module's
// table (auth.AdministeringRoles), which this schema does not have — the user
// module's migrations own users and nothing else — so the harness answers
// directly, through the one adapter the module ships for answering without a
// roles table.
func administering(context.Context, db.Tx[db.Tenant]) ([]string, error) {
	return []string{usertest.Administering}, nil
}

// newService is the service every case here is wired with: the floor included,
// because a test against a service with no floor would not be a test of the
// service the application composes.
func newService() *internal.Service {
	return internal.NewService(&contracts.AdministrationFunc{Ask: administering})
}

// TestServiceConforms runs the same suite the fake runs, against the real
// service, a real Postgres and a real tenant transaction.
func TestServiceConforms(t *testing.T) {
	usertest.RunService(t, func(t *testing.T, run func(usertest.Fixture)) {
		_, conn := dbtest.Schema(t, user.Migrations)
		svc := newService()
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(usertest.Fixture{
				Ctx: ctx, Tx: tx, Service: svc,
				Published: func() []string { return outbox(t, tx) },
				// What rest.Spec.deleteRow does, in the order it does it: the
				// row read and locked, the soft delete, then the hook that
				// carries the floor. The suite drives the same door the
				// generated route drives.
				Delete: func(id uuid.UUID) error {
					// The savepoint is what the request would be. kit/rest
					// deletes the row and then runs the hook, and a refusing
					// hook is an error out of the handler, which rolls the
					// request's transaction back; a suite that runs every case
					// in one transaction has to undo the write itself or the
					// row stays deleted after a refusal that the product would
					// have unwound.
					if err := tx.DB().SavePoint("beforedelete").Error; err != nil {
						return err
					}
					err := func() error {
						u, err := crud.GetForUpdate[*contracts.User](tx, id)
						if err != nil {
							return err
						}
						if err := crud.Delete[*contracts.User](tx, id, true); err != nil {
							return err
						}
						return svc.RefuseLastAdministrator(ctx, tx, u)
					}()
					if err != nil {
						if back := tx.DB().RollbackTo("beforedelete").Error; back != nil {
							return back
						}
					}
					return err
				},
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
	_, conn := dbtest.Schema(t, user.Migrations)
	svc := newService()

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
	admin, conn := dbtest.Schema(t, user.Migrations)
	svc := newService()
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
	admin, conn := dbtest.Schema(t, user.Migrations)
	svc := newService()

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

// TestAHandleIsPerTenantAndRenameableWithoutMovingThePerson is the pair of
// decisions the handle was built on, tested where the published conformance
// harness structurally cannot reach them: that harness is one tenant, so it can
// say "unique" but not "per tenant", and it has one transaction, so it cannot
// watch a rename against another tenant holding the same name.
func TestAHandleIsPerTenantAndRenameableWithoutMovingThePerson(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	svc := newService()

	var acmeID, globexID, probeID uuid.UUID
	asAcme := func(want string, fn func(context.Context, db.Tx[db.Tenant]) error) {
		t.Helper()
		if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, fn); err != nil && want != "" {
			t.Fatalf("%s: %v", want, err)
		}
	}

	asAcme("claim in acme", func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := svc.Invite(ctx, tx, "sam@acme.test", "Sam A")
		if err != nil {
			return err
		}
		acmeID = u.ID
		_, err = svc.SetHandle(ctx, tx, u.ID, " Sam ")
		return err
	})

	// The same name in the other tenant: per tenant is the decision, so this has to
	// succeed rather than discover somebody else's claim.
	if err := db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := svc.Invite(ctx, tx, "sam@globex.test", "Sam B")
		if err != nil {
			return err
		}
		globexID = u.ID
		if _, err = svc.SetHandle(ctx, tx, u.ID, "sam"); err != nil {
			return err
		}
		found, err := svc.ByHandle(ctx, tx, "sam")
		if err != nil {
			return err
		}
		if found.ID != globexID {
			t.Errorf("globex resolved sam to %s, want its own row", found.ID)
		}
		return nil
	}); err != nil {
		t.Fatalf("globex claiming the handle acme holds: %v", err)
	}

	asAcme("second claim in one tenant", func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := svc.Invite(ctx, tx, "sam2@acme.test", "Sam C")
		if err != nil {
			return err
		}
		if _, err := svc.SetHandle(ctx, tx, u.ID, "sam"); !errors.Is(err, crud.ErrConflict) {
			t.Errorf("a second claim in one tenant = %v, want ErrConflict", err)
		}
		found, err := svc.ByHandle(ctx, tx, "SAM")
		if err != nil {
			return err
		}
		if found.ID != acmeID {
			t.Errorf("ByHandle found %s, want acme's row and not globex's %s", found.ID, globexID)
		}
		return nil
	})

	// The rename: the handle moves, the person does not. Every foreign key, event
	// subject and audit row ever written names acmeID, and after this call it still
	// does — which is the whole argument for keeping the uuid as the key.
	asAcme("rename and release", func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		renamed, err := svc.SetHandle(ctx, tx, acmeID, "sam.example")
		if err != nil {
			return err
		}
		if renamed.ID != acmeID {
			t.Errorf("renaming moved the person from %s to %s", acmeID, renamed.ID)
		}
		if _, err := svc.ByHandle(ctx, tx, "sam"); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("the released handle still resolves to somebody: %v", err)
		}
		// Released rather than held-and-renamed, so the next person can have it —
		// which only works because the unique index is partial on the same rule the
		// service enforces.
		d, err := svc.Invite(ctx, tx, "sam3@acme.test", "Sam D")
		if err != nil {
			return err
		}
		if _, err := svc.SetHandle(ctx, tx, d.ID, "sam"); err != nil {
			return err
		}
		e, err := svc.Invite(ctx, tx, "sam4@acme.test", "Sam E")
		if err != nil {
			return err
		}
		probeID = e.ID
		return nil
	})

	// And the database, not the service, holds the line: E takes the name D holds,
	// through the raw table with no SetHandle and no Validate in between. It gets
	// its own transaction, because Postgres ends a transaction the moment it
	// refuses a statement, and a probe that poisons the block it is measuring
	// proves nothing about what came after it.
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("UPDATE users SET handle = 'sam' WHERE id = ?", probeID).Error
	}); err == nil {
		t.Error("a write through the raw table took a name somebody else holds in this tenant")
	}
}
