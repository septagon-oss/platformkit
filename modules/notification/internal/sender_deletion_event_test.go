package internal_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
)

func TestDeletingAPendingSenderPublishesItsAuditEvent(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	store := senders()
	ctx := asAdmin(tenancy.WithTenant(t.Context(), acme))
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := store.Put(ctx, tx, sender())
		if err != nil {
			return err
		}
		before := len(outbox(t, tx))
		if err := store.Delete(ctx, tx, row.ID); err != nil {
			return err
		}
		current, err := store.For(ctx, tx)
		if err != nil {
			return err
		}
		if current != nil {
			t.Fatal("delete did not remove the pending sender")
		}
		if after := len(outbox(t, tx)); after != before+1 {
			t.Errorf("sender deletion published %d events, want one transactional event for the audit subscriber", after-before)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
