package internal_test

import (
	"context"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

// TestTheEmailFieldOfANoticeAsksForTheChannelItStoodFor: a notice written before
// notices named their channels still asks for mail. The field is read, so the two
// spellings leave the same ledger, and a notice that names neither still asks for
// the row alone — the ceiling has a floor, not a default that reaches outside the
// application.
func TestTheEmailFieldOfANoticeAsksForTheChannelItStoodFor(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	svc := internal.NewService(directory{})

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		mail, err := svc.Notify(ctx, tx, contracts.Notice{
			Recipient: notificationtest.Ada, Title: "the flag", Email: true,
		})
		if err != nil {
			return err
		}
		asked := []string{"in_app requested", "in_app sent", "email requested"}
		if got := ledger(t, tx, mail.ID); !slices.Equal(got, asked) {
			t.Errorf("a notice that set Email left %v, want %v", got, asked)
		}
		if got, want := outbox(t, tx), []string{contracts.EventCreated, contracts.EventEmailRequested}; !slices.Equal(got, want) {
			t.Errorf("the flag published %v, want %v: the mail worker subscribes to the second name", got, want)
		}

		named, err := svc.Notify(ctx, tx, contracts.Notice{
			Recipient: notificationtest.Ada, Title: "the channel", Wants: contracts.WantsEmail,
		})
		if err != nil {
			return err
		}
		if got, want := ledger(t, tx, named.ID), ledger(t, tx, mail.ID); !slices.Equal(got, want) {
			t.Errorf("Email asks for %v and Wants: WantsEmail asks for %v; one ceiling cannot have two ledgers", want, got)
		}

		row, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "the row"})
		if err != nil {
			return err
		}
		if got, want := ledger(t, tx, row.ID), []string{"in_app requested", "in_app sent"}; !slices.Equal(got, want) {
			t.Errorf("a notice that asks for nothing leaves %v, want %v", got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("notifying three ways: %v", err)
	}
}
