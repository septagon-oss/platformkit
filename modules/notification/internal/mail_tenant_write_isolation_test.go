package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

func TestAnotherTenantCannotWriteAnOwedMail(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	svc := internal.NewService(directory{})
	queue(t, conn, svc)

	var intentID uuid.UUID
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT id FROM notification_mail_intents LIMIT 1").Row().Scan(&intentID)
	}); err != nil {
		t.Fatalf("read the source tenant's intent id: %v", err)
	}
	other := tenancy.Tenant{ID: uuid.New(), Slug: "other", Name: "Other"}
	if err := db.Run(tenancy.WithTenant(t.Context(), other), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		result := tx.DB().Exec("UPDATE notification_mail_intents SET state = ? WHERE id = ?", contracts.MailIntentSent, intentID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 0 {
			t.Errorf("another tenant changed %d owed mail rows", result.RowsAffected)
		}
		return nil
	}); err != nil {
		t.Fatalf("attempt the cross-tenant write: %v", err)
	}
	if state, _, _, _ := intentState(t, conn); state != contracts.MailIntentQueued {
		t.Fatalf("the source tenant's intent became %s after the foreign write, want queued", state)
	}
}
