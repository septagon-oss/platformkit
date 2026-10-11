package seed

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestSeedKeysAreIsolatedByDatabaseRLS(t *testing.T) {
	admin, app := dbtest.Schema(t)
	first := tenancy.Tenant{ID: uuid.New(), Slug: "first"}
	second := tenancy.Tenant{ID: uuid.New(), Slug: "second"}
	for _, tenant := range []tenancy.Tenant{first, second} {
		if _, err := admin.ExecContext(t.Context(), `INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $2)`, tenant.ID, tenant.Slug); err != nil {
			t.Fatal(err)
		}
	}
	resource := Resource{Module: "content", Entity: "content", NaturalKey: "slug"}
	rowID := uuid.New()
	if err := db.Run(tenancy.WithTenant(t.Context(), first), app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return putKey(tx, resource, "home", "starter", rowID)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), second), app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		key, owned, err := lookupKey(tx, resource, "home")
		if err != nil {
			return err
		}
		if owned || key.RecordID != uuid.Nil {
			t.Errorf("tenant B can read tenant A's key: %+v, owned=%t", key, owned)
		}
		// Even a raw write carrying A's ID is refused by the table policy.
		if err := tx.DB().Exec(`INSERT INTO seed_keys (tenant_id, module, entity, key, kind) VALUES (?, 'content', 'content', 'foreign', 'starter')`, first.ID).Error; err == nil {
			t.Error("tenant B wrote tenant A's key")
		}
		return nil // the failed statement makes this transaction roll back
	}); err == nil {
		t.Fatal("foreign write did not abort transaction")
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), first), app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		key, owned, err := lookupKey(tx, resource, "home")
		if err != nil {
			return err
		}
		if !owned || key.RecordID != rowID {
			t.Errorf("tenant A lost its key: %+v, owned=%t", key, owned)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
