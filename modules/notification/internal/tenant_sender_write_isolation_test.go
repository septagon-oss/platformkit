package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

// TestAnotherTenantWritesNothingOfThisTenants is the write half of the isolation
// this branch claims. migrations/000028 and 000029 give every table the branch
// adds the same USING/WITH CHECK pair 000011 and 000027 have, and internal.record
// hands the database the tenant of the transaction rather than one read from a
// row — so the promise is that a second tenant's transaction can neither see the
// first tenant's rows nor put a row, an update or a delete into them, including
// by primary key, which is the only shape of the attempt a table without a policy
// would survive. The existing cases (TestSettingsAreSomebodyElseInvisible,
// TestOneTenantsLedgerIsNotAnothers) read; this one writes.
//
// Each attempt gets its own transaction: a refused statement aborts the
// transaction it was made in, and the point of the case is the row, not the
// SQLSTATE.
func TestAnotherTenantWritesNothingOfThisTenants(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	store, admin := internal.Prefs{}, senders()
	var notice, senderID, prefID uuid.UUID

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := internal.NewService(directory{}, internal.WithPreferences(internal.Prefs{}),
			internal.WithSenders(admin)).Notify(ctx, tx, contracts.Notice{
			Recipient: notificationtest.Ada, Title: "acme's own", Wants: contracts.WantsInApp,
		})
		if err != nil {
			return err
		}
		notice = row.ID
		saved, err := admin.Put(ctx, tx, sender())
		if err != nil {
			return err
		}
		senderID = saved.ID
		choice, err := store.SetChannel(ctx, tx, notificationtest.Ada, "", contracts.ChannelEmail, false)
		if err != nil {
			return err
		}
		prefID = choice.ID
		return nil
	})
	if err != nil {
		t.Fatalf("acme's own writes: %v", err)
	}

	// Every attempt below runs as globex and must change nothing of acme's.
	attempts := []struct {
		what  string
		cross func(*gorm.DB) (int64, error)
	}{
		{"append to acme's delivery ledger", func(tx *gorm.DB) (int64, error) {
			res := tx.Exec(`INSERT INTO notification_deliveries (tenant_id, notification_id, channel, outcome, reason)
				VALUES (?, ?, 'email', 'suppressed', 'globex tried')`, acme.ID, notice)
			return res.RowsAffected, res.Error
		}},
		{"rename acme's sender", func(tx *gorm.DB) (int64, error) {
			res := tx.Exec(`UPDATE notification_senders SET from_name = 'Globex Billing' WHERE id = ?`, senderID)
			return res.RowsAffected, res.Error
		}},
		{"undo acme's opt-out", func(tx *gorm.DB) (int64, error) {
			res := tx.Exec(`UPDATE notification_preferences SET enabled = TRUE WHERE id = ?`, prefID)
			return res.RowsAffected, res.Error
		}},
		{"delete acme's quiet window", func(tx *gorm.DB) (int64, error) {
			res := tx.Exec(`DELETE FROM notification_quiet_hours WHERE tenant_id = ?`, acme.ID)
			return res.RowsAffected, res.Error
		}},
	}
	for _, a := range attempts {
		var changed int64
		err := db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			// A refused INSERT aborts the whole transaction; a savepoint keeps the
			// case about the row the policy did or did not let through.
			_ = tx.DB().Transaction(func(sp *gorm.DB) error {
				changed, _ = a.cross(sp)
				return errors.New("the attempt is over")
			})
			return nil
		})
		if err != nil {
			t.Fatalf("%s: %v", a.what, err)
		}
		if changed != 0 {
			t.Errorf("globex could %s: %d rows changed in acme's tables", a.what, changed)
		}
	}

	// The commands refuse an id they did not mint, the same answer as no id at all.
	if err := db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := admin.Verify(ctx, tx, senderID); err == nil {
			t.Error("globex verified acme's sender")
		}
		if err := admin.Delete(ctx, tx, senderID); err == nil {
			t.Error("globex deleted acme's sender by its id")
		}
		return nil
	}); err != nil {
		t.Fatalf("globex's commands against acme's rows: %v", err)
	}

	// Acme's rows are what acme wrote.
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		saved, err := admin.For(ctx, tx)
		if err != nil {
			return err
		}
		if saved == nil || saved.FromName != "Acme" {
			t.Errorf("acme's sender reads %+v after globex's attempts, want the row acme wrote", saved)
		}
		if _, total, err := store.Mine(ctx, tx, notificationtest.Ada, crud.Query{}); err != nil || total != 1 {
			t.Errorf("acme's person has %d preference rows after globex's attempt, want 1 (%v)", total, err)
		}
		rows, err := internal.Deliveries(tx, notice)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.Reason == "globex tried" {
				t.Error("acme's ledger carries a row globex wrote")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("acme's rows after the attempt: %v", err)
	}
}
