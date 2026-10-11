package main

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestSeedRechecksActorRolesAfterTheirRevocation(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	const email = "seed-writer@example.test"
	var tenant tenancy.Tenant
	var actorID uuid.UUID
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		tenant, err = c.tenants.ByHost(ctx, tx, acmeHost)
		if err != nil {
			return err
		}
		person, err := c.users.Provision(ctx, tx, tenant.ID, email, "Seed writer", "long enough for a demo password", []string{"admin"})
		if err == nil {
			actorID = person.ID
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	service, err := seed.New(seed.Deps{
		Files: fstest.MapFS{"seed/starter/contents.yaml": {Data: []byte("apiVersion: platformkit.seed/v1\nresource: contents\nrecords:\n  - key: revoked-writer-page\n    fields: {title: Must not be written}\n")}},
		Root:  "seed", Clock: seedClock{}, Writers: []seed.Writer{&contentSeeder{svc: c.contents}}, Authorize: seedGrants{auth: c.auth},
	})
	if err != nil {
		t.Fatal(err)
	}
	tenantCtx := tenancy.WithTenant(t.Context(), tenant)
	reachedApply := false
	// This is the same ordering as a run that waits after resolving --as:
	// another transaction commits the revocation before Apply checks its grants.
	err = db.Run(tenantCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		actorCtx, err := seedActor(ctx, c.users, tx, email)
		if err != nil {
			return err
		}
		if err := db.Run(tenantCtx, conn, func(ctx context.Context, other db.Tx[db.Tenant]) error {
			ctx, err := seedActor(ctx, c.users, other, adminEmail)
			if err != nil {
				return err
			}
			_, err = c.users.SetRoles(ctx, other, actorID, nil)
			return err
		}); err != nil {
			return err
		}
		person, err := c.users.Get(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if len(person.Roles) != 0 {
			t.Fatalf("revocation did not reach the seed transaction: roles=%v", person.Roles)
		}
		reachedApply = true
		plan, applyErr := service.Apply(actorCtx, tx, seed.Selection{})
		if applyErr == nil {
			t.Errorf("seed accepted a person whose roles were already revoked: %s", plan)
		} else if len(plan.Items) != 0 {
			t.Errorf("refused run returned a success plan: %s", plan)
		}
		return applyErr
	})
	if !reachedApply {
		t.Fatalf("could not reach Apply after a committed role revocation: %v", err)
	}
	// Inspect a new transaction: a correct refusal has rolled back all three.
	if readErr := db.Run(tenantCtx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		for _, query := range []string{
			"SELECT count(*) FROM contents WHERE slug = 'revoked-writer-page'",
			"SELECT count(*) FROM seed_keys WHERE key = 'revoked-writer-page'",
			"SELECT count(*) FROM platformkit_outbox WHERE payload->>'slug' = 'revoked-writer-page'",
		} {
			var count int
			if err := tx.DB().Raw(query).Row().Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				t.Errorf("revoked actor left %d committed rows for %s", count, query)
			}
		}
		return nil
	}); readErr != nil {
		t.Fatal(readErr)
	}
	if err != nil {
		t.Logf("run refused: %v", err)
	}
}
