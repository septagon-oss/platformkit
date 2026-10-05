package httpx_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAnotherTenantCannotReadOrChangeACommandClaim(t *testing.T) {
	_, router, f, _, path := setupNote(t)
	if res := send(t, router, path, keyA, "private response"); res.Code != http.StatusOK {
		t.Fatalf("first tenant command returned %d: %s", res.Code, res.Body)
	}
	other := tenancy.Tenant{ID: uuid.New(), Slug: "other"}
	err := db.Run(tenancy.WithTenant(t.Context(), other), f.app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var n int
		if err := tx.DB().Raw("SELECT count(*) FROM platformkit_idempotency WHERE tenant_id = ?", f.tenant.ID).Row().Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("another tenant read %d claims", n)
		}
		for _, statement := range []string{
			"UPDATE platformkit_idempotency SET response = 'stolen'::bytea WHERE tenant_id = ?",
			"DELETE FROM platformkit_idempotency WHERE tenant_id = ?",
		} {
			res := tx.DB().Exec(statement, f.tenant.ID)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 0 {
				t.Errorf("another tenant changed %d claims", res.RowsAffected)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := heldRows(t, f, true); n != 1 {
		t.Fatalf("another tenant removed the original claim: %d remain", n)
	}
}
