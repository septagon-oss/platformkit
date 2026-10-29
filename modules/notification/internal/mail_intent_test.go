package internal_test

// The owed mail, against the module's own schema: the conformance suite over the SQL
// service, and the cases a fake cannot answer because they are about which
// transaction wrote the row and about what a process that died across the socket left.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

// rollBack is the sentinel that ends a conformance case's transaction without keeping
// its rows: the next case starts from the same schema, not from the last case's data.
var rollBack = errors.New("conformance case finished")

func TestTheMailIntentServiceOwesWhatItIsTold(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	svc := internal.NewService(directory{})
	notificationtest.RunMailIntents(t, func(t *testing.T, run func(notificationtest.IntentFixture)) {
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(notificationtest.IntentFixture{Ctx: ctx, Tx: tx, Intents: svc})
			return rollBack
		})
		if !errors.Is(err, rollBack) {
			t.Fatalf("the case's transaction: %v", err)
		}
	})
}

// TestAnAmbiguousSendIsUnknownAndIsNotSentAgainWithinTheDelay is the brief's crash
// case. The relay may have taken the message and this process cannot say which; the
// intent says so, and the rule — one re-send, never before the delay — is what stops
// the row becoming a mail per tick.
func TestAnAmbiguousSendIsUnknownAndIsNotSentAgainWithinTheDelay(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	relay := &ambiguous{}
	now := time.Now().UTC()
	svc := internal.NewService(directory{}, internal.WithMailer(relay), internal.WithClock(func() time.Time { return now }))
	queue(t, conn, svc)

	drainMail(t, conn, svc)
	if got := relay.n.Load(); got != 1 {
		t.Fatalf("the relay was handed %d messages, want 1", got)
	}
	if state, uncertain, attempt, _ := intentState(t, conn); state != contracts.MailIntentUnknown || uncertain != 1 || attempt != 1 {
		t.Fatalf("an ambiguous send left the intent %s, uncertain %d, attempt %d, want unknown/1/1", state, uncertain, attempt)
	}

	// The same tick, and then the minute and the hour after it: nothing is due.
	for _, later := range []time.Duration{0, time.Minute, 10 * time.Minute, 23 * time.Hour} {
		now = now.Add(later)
		drainMail(t, conn, svc)
		if got := relay.n.Load(); got != 1 {
			t.Fatalf("after %s the relay was handed %d messages, want the one ambiguous send", later, got)
		}
	}

	// The delay arrives, and with it the one re-send the rule allows.
	now = now.Add(time.Hour)
	drainMail(t, conn, svc)
	if got := relay.n.Load(); got != 2 {
		t.Fatalf("after the unknown delay the relay was handed %d messages, want the one re-send", got)
	}
	if state, uncertain, _, _ := intentState(t, conn); state != contracts.MailIntentUnknown || uncertain != 2 {
		t.Fatalf("the second ambiguity left the intent %s uncertain %d, want unknown with 2", state, uncertain)
	}
	// And never again, however long the queue waits.
	for range 3 {
		now = now.Add(240 * time.Hour)
		drainMail(t, conn, svc)
	}
	if got := relay.n.Load(); got != 2 {
		t.Errorf("an intent that spent its re-send was sent %d times, want 2", got)
	}
}

// TestAnExpiredAttemptIsUnknownAndNotBlindlyReSent is the crash between the send and
// the outcome: the relay took the message, the worker died before writing that it
// did, and the lease found the row. Synchronised by channels — the relay says when it
// was called and holds the attempt until this case says it may finish.
func TestAnExpiredAttemptIsUnknownAndNotBlindlyReSent(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	relay := &blocking{started: make(chan struct{}), finish: make(chan struct{})}
	svc := internal.NewService(directory{}, internal.WithMailer(relay))
	queue(t, conn, svc)

	done := make(chan error, 1)
	go func() {
		done <- internal.Deliver(svc, acmeOnly, time.Second, 10, time.Millisecond).Run(tenancy.WithTenant(t.Context(), acme), conn)
	}()
	select {
	case <-relay.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker never reached the relay")
	}
	// The lease has expired while the socket is still open, and a second tick finds
	// the row. It does not re-send: it records that the answer never came.
	drainMailLeased(t, conn, svc, time.Millisecond)
	if got := relay.count(); got != 1 {
		t.Errorf("an expired attempt was re-sent: the relay saw %d messages, want the one that was already on the wire", got)
	}
	if state, uncertain, _, _ := intentState(t, conn); state != contracts.MailIntentUnknown || uncertain != 1 {
		t.Errorf("the expired attempt is %s uncertain %d, want unknown with 1", state, uncertain)
	}
	close(relay.finish)
	if err := <-done; err != nil {
		t.Fatalf("the first worker: %v", err)
	}
}

// TestAMailIsNotSentToAnAddressNobodyDecidedFor closes the queue's gap: the person
// moved (or was erased) after the mail was written, and the old address is not
// anybody's to write to now.
func TestAMailIsNotSentToAnAddressNobodyDecidedFor(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	relay := &countedSend{}
	moved := &renaming{directory: directory{}, to: "someone.else@acme.example.com"}
	svc := internal.NewService(moved, internal.WithMailer(relay))
	queue(t, conn, svc)

	drainMail(t, conn, svc)
	if got := relay.n.Load(); got != 0 {
		t.Errorf("the moved address got %d messages, want none", got)
	}
	if state, _, _, reason := intentState(t, conn); state != contracts.MailIntentSuppressed || reason == "" {
		t.Errorf("a moved address left the intent %s (%q), want suppressed with the reason", state, reason)
	}
}

// TestANoticeDeletedBeforeItsMailIsSentIsSuppressed: the mail was owed to a row that
// is gone, and there is nothing left to say.
func TestANoticeDeletedBeforeItsMailIsSentIsSuppressed(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	relay := &countedSend{}
	svc := internal.NewService(directory{}, internal.WithMailer(relay))
	noticeID := queue(t, conn, svc)

	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("UPDATE notifications SET deleted_at = now() WHERE id = ?", noticeID).Error
	}); err != nil {
		t.Fatalf("delete the notice: %v", err)
	}
	drainMail(t, conn, svc)
	if got := relay.n.Load(); got != 0 {
		t.Errorf("a deleted notice was mailed %d times, want none", got)
	}
	if state, _, _, reason := intentState(t, conn); state != contracts.MailIntentSuppressed || reason == "" {
		t.Errorf("a deleted notice left the intent %s (%q), want suppressed with the reason", state, reason)
	}
}

// TestAnotherTenantsOwedMailIsNotThere is the row-level-security case: the worker of
// tenant B walks, and tenant A's row is not in its world even though both were
// written through the same connection.
func TestAnotherTenantsOwedMailIsNotThere(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	relay := &countedSend{}
	svc := internal.NewService(directory{}, internal.WithMailer(relay))
	queue(t, conn, svc)

	other := tenancy.Tenant{ID: uuid.New(), Slug: "other", Name: "Other"}
	if err := internal.Deliver(svc, oneTenant{[]tenancy.Tenant{other}}, time.Second, 10, time.Minute).
		Run(tenancy.WithTenant(t.Context(), other), conn); err != nil {
		t.Fatalf("drain as the other tenant: %v", err)
	}
	if got := relay.n.Load(); got != 0 {
		t.Errorf("another tenant's worker sent %d messages, want none", got)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), other), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var owed int64
		if err := tx.DB().Raw("SELECT count(*) FROM notification_mail_intents").Scan(&owed).Error; err != nil {
			return err
		}
		if owed != 0 {
			t.Errorf("the other tenant can see %d of acme's owed mails", owed)
		}
		return nil
	}); err != nil {
		t.Fatalf("read as the other tenant: %v", err)
	}
}

// TestTheRetentionSweepForgetsASentMailAndNeverAnUnknownOne is the stated retention:
// a finished intent ages out, and a mail whose fate nobody knows does not.
func TestTheRetentionSweepForgetsASentMailAndNeverAnUnknownOne(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	svc := internal.NewService(directory{})
	_ = queue(t, conn, svc)
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		// One sent intent a quarter of a year old, and one unknown the same age. The
		// cutoff is the database's, so the rows are aged in the database's units too.
		return tx.DB().Exec("UPDATE notification_mail_intents SET state = 'sent', updated_at = now() - interval '91 days'").Error
	}); err != nil {
		t.Fatalf("age the intent: %v", err)
	}

	if err := internal.MailRetention(acmeOnly, 0).Run(tenancy.WithTenant(t.Context(), acme), conn); err != nil {
		t.Fatalf("run the sweep: %v", err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var kept int64
		if err := tx.DB().Raw("SELECT count(*) FROM notification_mail_intents").Scan(&kept).Error; err != nil {
			return err
		}
		if kept != 0 {
			t.Errorf("the sweep kept %d of the finished rows, want none", kept)
		}
		return nil
	}); err != nil {
		t.Fatalf("read after the sweep: %v", err)
	}

	queue(t, conn, svc)
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("UPDATE notification_mail_intents SET state = 'unknown', updated_at = now() - interval '91 days' WHERE state = 'queued'").Error
	}); err != nil {
		t.Fatalf("age an unknown intent: %v", err)
	}
	if err := internal.MailRetention(acmeOnly, 0).Run(tenancy.WithTenant(t.Context(), acme), conn); err != nil {
		t.Fatalf("run the sweep again: %v", err)
	}
	if state, _, _, _ := intentState(t, conn); state != contracts.MailIntentUnknown {
		t.Errorf("the sweep forgot an unresolved mail: it is %s", state)
	}
}

// queue writes one intent for Ada's notice and returns the notice's id.
func queue(t *testing.T, conn *db.Conn, svc *internal.Service) uuid.UUID {
	t.Helper()
	var noticeID uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "A thing happened", Wants: contracts.WantsEmail})
		if err != nil {
			return err
		}
		noticeID = row.ID
		_, _, err = svc.Queue(ctx, tx, contracts.OutgoingMail{
			Purpose: contracts.EventEmailRequested, Cause: uuid.NewString(), Person: notificationtest.Ada,
			Notice: row.ID, Message: contracts.Message{To: notificationtest.AdaEmail, Subject: "A thing happened", Body: "It happened."},
		})
		return err
	})
	if err != nil {
		t.Fatalf("queue the mail: %v", err)
	}
	return noticeID
}

// intentState is the one row this fixture owes, as a reviewer reads it.
func intentState(t *testing.T, conn *db.Conn) (state string, uncertain, attempt int, reason string) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT state, uncertain, attempt, reason FROM notification_mail_intents ORDER BY seq DESC LIMIT 1").
			Row().Scan(&state, &uncertain, &attempt, &reason)
	})
	if err != nil {
		t.Fatalf("read the intent: %v", err)
	}
	return state, uncertain, attempt, reason
}

// ambiguous is a relay that takes the message and never says so.
type ambiguous struct{ n atomic.Int64 }

func (a *ambiguous) Send(context.Context, contracts.Message) error {
	a.n.Add(1)
	return contracts.ErrUncertainSend
}

// countedSend is a relay that takes everything and counts what it was handed.
type countedSend struct{ n atomic.Int64 }

func (c *countedSend) Send(context.Context, contracts.Message) error {
	c.n.Add(1)
	return nil
}

// blocking is a relay that says when the message arrived and holds the attempt open,
// which is how a case stands in the middle of a send without sleeping.
type blocking struct {
	n       atomic.Int64
	started chan struct{}
	finish  chan struct{}
}

func (b *blocking) Send(context.Context, contracts.Message) error {
	if b.n.Add(1) == 1 {
		close(b.started)
	}
	<-b.finish
	return nil
}

func (b *blocking) count() int64 { return b.n.Load() }

// renaming answers one address until the tenant's person moves, which is the answer
// the worker re-reads before the socket opens.
type renaming struct {
	directory
	to string
}

func (r *renaming) Email(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (string, error) {
	return r.to, nil
}
