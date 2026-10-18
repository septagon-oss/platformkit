package internal_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

func TestACommittedEmailOptOutKeepsTheNoticeAndClosesItsLedger(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	ctx := tenancy.WithTenant(t.Context(), acme)
	svc := settings()
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := notification.Settings().SetChannel(ctx, tx, notificationtest.Ada, "", contracts.ChannelEmail, false)
		return err
	}); err != nil {
		t.Fatalf("commit opt-out: %v", err)
	}
	var id uuid.UUID
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Notify(ctx, tx, contracts.Notice{
			Recipient: notificationtest.Ada, Title: "A decision was made", Wants: contracts.WantsEmail,
		})
		if err == nil {
			id = row.ID
		}
		return err
	}); err != nil {
		t.Fatalf("commit notice: %v", err)
	}
	if id == uuid.Nil {
		t.Fatal("successful notice has no identifier")
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, total, err := svc.ListFor(ctx, tx, notificationtest.Ada, crud.Query{})
		if err != nil {
			return err
		}
		if total != 1 || len(rows) != 1 || rows[0].ID != id {
			t.Errorf("committed inbox = %v, total %d; want notice %s", rows, total, id)
		}
		want := []string{"in_app requested", "in_app sent", "email requested", "email suppressed"}
		if got := ledger(t, tx, id); !slices.Equal(got, want) {
			t.Errorf("committed ledger = %v, want %v", got, want)
		}
		if got := reason(t, tx, id, internal.ChannelEmail, contracts.OutcomeSuppressed); got == "" {
			t.Error("email suppression has no reason")
		}
		if got, want := outbox(t, tx), []string{contracts.EventPreferenceSet, contracts.EventCreated}; !slices.Equal(got, want) {
			t.Errorf("committed events = %v, want %v without a mail request", got, want)
		}
		requested, terminal, err := internal.Coverage(tx)
		if err == nil && (requested != 2 || terminal != 2) {
			t.Errorf("committed coverage = %d/%d, want 2/2", terminal, requested)
		}
		return err
	}); err != nil {
		t.Fatalf("read committed delivery: %v", err)
	}
}
