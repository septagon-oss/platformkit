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

// globexSenderOwner is the caller globex's transactions name: a person of its
// own, the one a role would hold sender:manage for. It exists so that nothing in
// this case is refused for the wrong reason — Put, Verify and Delete answer a
// caller the transaction does not name with a policy refusal, and a cross-tenant
// attempt made by such a caller would prove only that. With both transactions
// naming somebody, the only thing left that can refuse is the tenant policy on
// the row. Acme's caller is the one every other case here already uses.
var globexSenderOwner = uuid.MustParse("7c1f0f0a-0000-4000-8000-0000000000b2")

// TestAnotherTenantThatHoldsTheKeyWritesNothingOfThisTenants is the isolation
// half of the sender conversation, with the caller question taken off the table.
//
// The ledger, the preferences and the quiet window are three tables the branch
// adds, each with its own row-level security policy on tenant_id (migrations
// 000027, 000028, 000029), and the commands write the transaction's tenant
// rather than one read from a row. What this case pins is that the separation
// comes from the policy and not from an accident of who was asking: globex names
// a caller of its own — one that would be granted sender:manage, which is the
// key the face is guarded by — and still changes nothing of acme's, by primary
// key, by append and by delete.
//
// The commands over an id globex did not mint answer the way an id nobody ever
// minted does (crud.ErrNotFound), which is the point: the refusal must not leak
// that acme has a sender, and it must not need the actor rule to hold.
//
// Each attempt gets its own transaction, because a refused statement aborts the
// one it was made in and the case is about the row, not the SQLSTATE.
func TestAnotherTenantThatHoldsTheKeyWritesNothingOfThisTenants(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	store, admin := internal.Prefs{}, senders()
	var notice, senderID, prefID uuid.UUID

	// Acme, through its own named caller, writes the three rows the attempts
	// below reach for.
	err := db.Run(asAdmin(tenancy.WithTenant(t.Context(), acme)), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
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

	// Every attempt below runs as globex, as a caller globex can name, and must
	// change nothing of acme's.
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
		err := db.Run(tenancy.WithActor(tenancy.WithTenant(t.Context(), globex), globexSenderOwner), conn,
			func(_ context.Context, tx db.Tx[db.Tenant]) error {
				// A refused INSERT aborts the whole transaction; a savepoint keeps
				// the case about the row the policy did or did not let through.
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

	// Over the commands, globex's caller is somebody, so the only answer left is
	// the one the row's tenant gives: acme's sender is nobody's, and the sentence
	// is the one an id that never existed gets.
	if err := db.Run(tenancy.WithActor(tenancy.WithTenant(t.Context(), globex), globexSenderOwner), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if _, err := admin.Verify(ctx, tx, senderID); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("globex verified acme's sender: got %v, want the answer an unknown id gets", err)
			}
			if err := admin.Delete(ctx, tx, senderID); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("globex deleted acme's sender by its id: got %v, want the answer an unknown id gets", err)
			}
			// The one write globex may make is its own: Put takes the tenant from
			// the transaction, never from a row it read, so the call that changes
			// nothing of acme's lands in globex's own tenant.
			saved, err := admin.Put(ctx, tx, sender())
			if err != nil {
				return err
			}
			if saved.TenantID != globex.ID {
				t.Errorf("globex's Put wrote a sender for tenant %s, want %s", saved.TenantID, globex.ID)
			}
			live, err := admin.For(ctx, tx)
			if err != nil || live == nil || live.TenantID != globex.ID {
				t.Errorf("globex's own sender reads %+v after its Put (%v)", live, err)
			}
			return nil
		}); err != nil {
		t.Fatalf("globex's commands against acme's rows: %v", err)
	}

	// Acme's rows are what acme wrote.
	err = db.Run(asAdmin(tenancy.WithTenant(t.Context(), acme)), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			saved, err := admin.For(ctx, tx)
			if err != nil {
				return err
			}
			if saved == nil || saved.FromName != "Acme" || saved.Status != contracts.SenderPending {
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
