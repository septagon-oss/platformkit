package internal_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

func TestQueuedMailRechecksSenderVerificationBeforeDelivery(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	store := senders()
	mailbox := notification.NewMailbox()
	svc := internal.NewService(directory{}, internal.WithSenders(store))
	send := internal.SendMail(mailbox, directory{}, hosts{}, store, true)
	ctx := asAdmin(tenancy.WithTenant(t.Context(), acme))
	var noticeID uuid.UUID
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := store.Put(ctx, tx, sender())
		if err != nil {
			return err
		}
		if _, err := store.Verify(ctx, tx, row.ID); err != nil {
			return err
		}
		notice, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "Queued while verified", Wants: contracts.WantsEmail})
		if err == nil {
			noticeID = notice.ID
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		replacement := sender()
		replacement.Selector = "replacement"
		_, err := store.Put(ctx, tx, replacement)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		current, err := store.For(ctx, tx)
		if err != nil {
			return err
		}
		if current == nil || current.CanSend() {
			t.Fatal("fixture must have a sender that is no longer verified")
		}
		return send.Handler(ctx, tx, request(noticeID, notificationtest.Ada))
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(mailbox.Sent()); got != 0 {
		t.Errorf("queued mail sent %d messages using an unverified sender", got)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows := ledger(t, tx, noticeID)
		if slices.Contains(rows, "email sent") {
			t.Errorf("unverified sender was recorded as sent: %v", rows)
		}
		if !slices.Contains(rows, "email suppressed") && !slices.Contains(rows, "email failed") {
			t.Errorf("unverified sender has no terminal refusal: %v", rows)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
