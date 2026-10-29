package events_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAnotherTenantCannotReadOrClearAnOwedEffect(t *testing.T) {
	_, conn := dbtest.Schema(t)
	owner, other := deferredTenant(t, conn), deferredTenant(t, conn)
	id := uuid.New()
	ownerCtx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: owner})
	if err := db.Run(ownerCtx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(`INSERT INTO platformkit_handled (event_id, durable, tenant_id, effects_owed)
		 VALUES (?, 'cart-cart-order-placed', ?, 2)`, id, owner).Error
	}); err != nil {
		t.Fatal(err)
	}
	otherCtx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: other})
	err := db.Run(otherCtx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var owed int
		return tx.DB().Raw("SELECT effects_owed FROM platformkit_handled WHERE event_id = ?", id).Row().Scan(&owed)
	})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("another tenant's read = %v, want no visible row", err)
	}
	if err := db.Run(otherCtx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		result := tx.DB().Exec("UPDATE platformkit_handled SET effects_owed = 0 WHERE event_id = ?", id)
		if result.RowsAffected != 0 {
			t.Errorf("another tenant changed %d owed claims", result.RowsAffected)
		}
		return result.Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ownerCtx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var owed int
		if err := tx.DB().Raw("SELECT effects_owed FROM platformkit_handled WHERE event_id = ?", id).Row().Scan(&owed); err != nil {
			return err
		}
		if owed != 2 {
			t.Errorf("owner's effects owed = %d after another tenant's write, want 2", owed)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
