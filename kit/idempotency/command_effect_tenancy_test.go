package idempotency_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestCommandEffectCannotBeReadOrChangedByAnotherTenant(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	store := idempotency.Postgres(httpx.ConnFrom)
	s := scope("scoped-effect")
	claim := uuid.New()
	owner := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: s.Tenant, Slug: "acme"})
	if err := db.Run(owner, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return store.Mark(ctx, tx, s, claim)
	}); err != nil {
		t.Fatal(err)
	}
	stranger := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: other, Slug: "other"})
	if err := db.Run(stranger, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var visible int64
		if err := tx.DB().Table("platformkit_command_effect").Count(&visible).Error; err != nil {
			return err
		}
		if visible != 0 {
			t.Errorf("other tenant read %d markers, want none", visible)
		}
		for _, query := range []string{
			"UPDATE platformkit_command_effect SET claim = ? WHERE tenant_id = ?",
			"DELETE FROM platformkit_command_effect WHERE claim = ? AND tenant_id = ?",
		} {
			result := tx.DB().Exec(query, claim, s.Tenant)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 0 {
				t.Errorf("other tenant changed %d markers, want none", result.RowsAffected)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(stranger, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return store.Mark(ctx, tx, s, uuid.New())
	}); err == nil {
		t.Error("another tenant placed a marker for the owner's scope")
	}
	var kept uuid.UUID
	if err := admin.QueryRowContext(t.Context(), "SELECT claim FROM platformkit_command_effect WHERE tenant_id = $1", s.Tenant).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != claim {
		t.Errorf("owner's marker = %s, want unchanged %s", kept, claim)
	}
}
