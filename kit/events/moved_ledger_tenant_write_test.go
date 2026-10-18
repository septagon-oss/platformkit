package events_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAnotherTenantCannotReadOrRewriteAMovedLedger(t *testing.T) {
	_, conn := dbtest.Schema(t)
	owner := tenantID(t, conn, "shop", "collect")
	other := tenantID(t, conn, "other-shop", "collect")
	id := uuid.New()
	old := appname.Durable("", ledgerModule, ledgerEvent)
	claimRow(t, conn, old, id, owner)
	deadRow(t, conn, old, id, owner)
	if _, err := events.MoveLedger(t.Context(), conn, "collect", "boot"); err != nil {
		t.Fatal(err)
	}
	want := appname.Durable("collect", ledgerModule, ledgerEvent)
	ctx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: other, Slug: "other-shop"})
	for _, table := range []string{"platformkit_handled", "platformkit_dead_letters"} {
		if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			var visible int64
			if err := tx.DB().Raw(`SELECT count(*) FROM `+table+` WHERE event_id = ?`, id).Scan(&visible).Error; err != nil {
				return err
			}
			if visible != 0 {
				t.Errorf("the other tenant reads %d rows of %s", visible, table)
			}
			changed := tx.DB().Exec(`UPDATE `+table+` SET durable = ? WHERE event_id = ?`,
				appname.Durable("academy", ledgerModule, ledgerEvent), id)
			if changed.RowsAffected != 0 {
				t.Errorf("the other tenant rewrote %d rows of %s", changed.RowsAffected, table)
			}
			return changed.Error
		}); err != nil {
			t.Fatal(err)
		}
		if got := durables(t, conn, table, id); len(got) != 1 || got[0] != want {
			t.Errorf("after the foreign write %s holds %v, want the original moved row at %s", table, got, want)
		}
	}
}
