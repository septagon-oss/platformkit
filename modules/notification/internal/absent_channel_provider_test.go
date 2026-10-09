package internal_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

func TestAnAbsentProviderClosesItsChannelAsSuppressed(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	svc, mod := notification.Module(notification.Deps{
		Recipients: directory{}, Mailer: notification.NewMailbox(),
		Providers: contracts.Providers{contracts.ChannelPush: nil},
	})
	for _, sub := range mod.Subscriptions {
		if sub.Name == contracts.EventPushRequested {
			t.Fatal("a nil provider must not have a push subscription")
		}
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Notify(ctx, tx, contracts.Notice{
			Recipient: notificationtest.Ada, Title: "Delivery without a carrier", Wants: contracts.WantsPush,
		})
		if err != nil {
			return err
		}
		rows, err := internal.Deliveries(tx, row.ID)
		if err != nil {
			return err
		}
		suppressed := false
		for _, r := range rows {
			if r.Channel == string(contracts.ChannelPush) && r.Outcome == contracts.OutcomeSuppressed && r.Reason != "" {
				suppressed = true
			}
		}
		if !suppressed {
			t.Errorf("absent push carrier left ledger %v; want a terminal suppression with a reason", ledger(t, tx, row.ID))
		}
		requested, terminal, err := internal.Coverage(tx)
		if err == nil && requested != terminal {
			t.Errorf("delivery_ledger_coverage = %d/%d; no worker exists to close the missing channel", terminal, requested)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
