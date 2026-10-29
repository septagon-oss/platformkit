package internal_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

// failing is a relay whose answer says nothing about what it did with the message.
type failing struct{}

func (failing) Send(context.Context, contracts.Message) error { return errors.New("relay refused") }

// refused is a relay that answered and did not take the message: a 5xx, which is a
// fact about the relay and therefore retryable on the ladder.
type refused struct{}

func (refused) Send(context.Context, contracts.Message) error {
	return contracts.Permanent("550 relay refused the message")
}

func ledger(t *testing.T, tx db.Tx[db.Tenant], id uuid.UUID) []string {
	t.Helper()
	rows, err := internal.Deliveries(tx, id)
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	var out []string
	for _, r := range rows {
		out = append(out, r.Channel+" "+r.Outcome)
	}
	return out
}

func request(id, recipient uuid.UUID) events.Event {
	return events.Event{ID: uuid.New(), Name: contracts.EventEmailRequested,
		Payload: []byte(`{"notificationId":"` + id.String() + `","recipientId":"` + recipient.String() + `"}`)}
}

// TestEveryRequestedChannelEndsInTheLedger is the ledger's first promise: a notice that
// asks for a channel leaves a `requested` row, and the step that finishes the channel
// leaves a terminal one — sent, or suppressed with the reason — in the same transaction,
// so "was Ada told" is a query and delivery_ledger_coverage is computable.
func TestEveryRequestedChannelEndsInTheLedger(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	svc := internal.NewService(directory{}, internal.WithMailer(notification.NewMailbox()))
	send := internal.SendMail(svc, hosts{}, true)
	var adaMail uuid.UUID

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		// Ada has an address: in-app is delivered with the row, email when the worker sends.
		ada, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "for Ada", Wants: contracts.WantsEmail})
		if err != nil {
			return err
		}
		if got, want := ledger(t, tx, ada.ID), []string{"in_app requested", "in_app sent", "email requested"}; !slices.Equal(got, want) {
			t.Errorf("after Notify Ada's ledger is %v, want %v", got, want)
		}
		if err := send.Handler(ctx, tx, request(ada.ID, ada.RecipientID)); err != nil {
			return err
		}
		// The decision is written, not sent: inside this transaction the mail is owed
		// and the channel is still open, which is what makes a rollback of this step
		// send nothing.
		if got := ledger(t, tx, ada.ID); !slices.Equal(got[len(got)-1:], []string{"email requested"}) {
			t.Errorf("after the mail request Ada's ledger is %v, want it to end in email requested", got)
		}
		adaMail = ada.ID

		// Bob has no address: the channel is suppressed at once, with the reason.
		bob, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Bob, Title: "for Bob", Wants: contracts.WantsEmail})
		if err != nil {
			return err
		}
		rows, err := internal.Deliveries(tx, bob.ID)
		if err != nil {
			return err
		}
		last := rows[len(rows)-1]
		if last.Channel != "email" || last.Outcome != "suppressed" || last.Reason == "" {
			t.Errorf("Bob's email ended as %+v, want suppressed with a reason", last)
		}

		// A notice that asks only for in-app requests no email at all.
		quiet, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "in-app only"})
		if err != nil {
			return err
		}
		if got := ledger(t, tx, quiet.ID); !slices.Equal(got, []string{"in_app requested", "in_app sent"}) {
			t.Errorf("an in-app notice's ledger is %v", got)
		}

		requested, terminal, err := internal.Coverage(tx)
		if err != nil {
			return err
		}
		// Four of five: Ada's mail is owed and un-sent, which is what the fifth row
		// means. An owed mail leaves the channel honestly unaccounted-for rather than
		// optimistically closed, and the drain below is what closes it.
		if requested != 5 || terminal != 4 {
			t.Errorf("coverage is %d of %d requested channels, want 4 of 5", terminal, requested)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("the transaction: %v", err)
	}
	// The first notice's mail was rolled back with its transaction, so the worker
	// has nothing to send: the row it would have claimed is not there.
	drainMail(t, conn, svc)
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var owed int64
		if err := tx.DB().Raw("SELECT count(*) FROM notification_mail_intents").Scan(&owed).Error; err != nil {
			return err
		}
		if owed != 0 {
			t.Errorf("a rolled-back decision left %d owed mails, want none", owed)
		}
		return nil
	}); err != nil {
		t.Fatalf("after the drain: %v", err)
	}
	_ = adaMail
}

// TestADeletedNoticeIsSuppressedAndAFailedSendLeavesNoFalseRow covers the two other ends:
// a notice deleted before its mail is due is suppressed with the reason, and a relay that
// refuses leaves no terminal row at all — its transaction rolls back and the outbox retries,
// so the ledger can never say "sent" for a message that was not, and the channel shows as
// uncovered until it finishes.
func TestARefusedSendLeavesNoTerminalRowUntilTheRelayAnswers(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	svc := internal.NewService(directory{}, internal.WithMailer(refused{}))
	var id uuid.UUID

	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "doomed", Wants: contracts.WantsEmail})
		id = row.ID
		if err != nil {
			return err
		}
		return internal.SendMail(svc, hosts{}, true).Handler(ctx, tx, request(id, notificationtest.Ada))
	}); err != nil {
		t.Fatalf("queue the mail: %v", err)
	}
	// The relay refuses every message. The attempt is recorded on the intent and the
	// channel stays open: nothing here may say "sent" for a message nobody took.
	drainMail(t, conn, svc)

	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if got := ledger(t, tx, id); slices.Contains(got, "email sent") {
			t.Errorf("a refused send left %v", got)
		}
		requested, terminal, err := internal.Coverage(tx)
		if err != nil {
			return err
		}
		if requested != 2 || terminal != 1 {
			t.Errorf("coverage after a refused send is %d of %d, want 1 of 2: the email is not accounted for yet", terminal, requested)
		}
		var state string
		var attempt int
		if err := tx.DB().Raw("SELECT state, attempt FROM notification_mail_intents").Row().Scan(&state, &attempt); err != nil {
			return err
		}
		if state != contracts.MailIntentQueued || attempt != 1 {
			t.Errorf("a refused first attempt leaves the intent %s at attempt %d, want it re-queued after one socket", state, attempt)
		}
		return nil
	}); err != nil {
		t.Fatalf("after the refusal: %v", err)
	}
}

// TestOneTenantsLedgerIsNotAnothers: the ledger is under the same row-level security as the
// notices, so another tenant's transaction reads none of it.
func TestOneTenantsLedgerIsNotAnothers(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	svc := internal.NewService(directory{})
	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "acme only"})
		id = row.ID
		return err
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	err = db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if got := ledger(t, tx, id); len(got) != 0 {
			t.Errorf("globex reads acme's ledger: %v", got)
		}
		requested, _, err := internal.Coverage(tx)
		if requested != 0 {
			t.Errorf("globex's coverage counts %d of acme's channels", requested)
		}
		return err
	})
	if err != nil {
		t.Fatalf("read in globex: %v", err)
	}
}
