package internal_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

func TestAnotherTenantCannotReadOrRewriteAFailedMail(t *testing.T) {
	_, conn := dbtest.Schema(t, notificationmodule.Migrations)
	ledger := mailLedger()
	const request = "failed-mail-owned-by-acme"
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return ledger.RecordMail(ctx, tx, notification.MailRecord{
			Kind: contracts.MailSetPassword, Recipient: "ada@acme.localhost", Outcome: notification.MailFailed,
			Reason: "connection refused", RequestID: request,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		outcome, known, err := ledger.MailOutcome(ctx, tx, request)
		if err != nil || known || outcome != "" {
			t.Errorf("other tenant read outcome=%q known=%t error=%v", outcome, known, err)
		}
		result := tx.DB().Exec("UPDATE direct_mail_deliveries SET outcome = 'sent', reason = '' WHERE request_id = ?", request)
		if result.RowsAffected != 0 {
			t.Errorf("other tenant rewrote %d delivery rows", result.RowsAffected)
		}
		return result.Error
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("INSERT INTO direct_mail_deliveries (tenant_id, recipient, kind, outcome, request_id) VALUES (?, ?, ?, 'sent', ?)",
			acme.ID, "ada@acme.localhost", contracts.MailSetPassword, request).Error
	})
	if err == nil {
		t.Error("RLS accepted an insert naming another tenant")
	}
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		outcome, known, err := ledger.MailOutcome(ctx, tx, request)
		if outcome != notification.MailFailed || !known {
			t.Errorf("owner's delivery changed: outcome=%q known=%t", outcome, known)
		}
		var rows int64
		if err := tx.DB().Table("direct_mail_deliveries").Count(&rows).Error; err != nil {
			return err
		}
		if rows != 1 {
			t.Errorf("owner has %d rows after the refused insert, want 1", rows)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
