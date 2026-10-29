package internal

// The two facts a mail that is owed has to hold, checked where the writer lives: a
// decision that rolls back owes nothing, and a decision that committed is still owed
// to a process that never saw it. Both are Postgres facts — no fake can hold them,
// because neither is about the row's shape but about which transaction wrote it.

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// intentSchema is the module's own SQL read from disk: this white-box test cannot
// import the package that embeds it.
func intentSchema(t *testing.T) *db.Conn {
	t.Helper()
	_, conn := dbtest.Schema(t, db.MigrationSource{Owner: "notification", Files: os.DirFS("../migrations")})
	return conn
}

// counted is a mailer that says how many messages it was handed.
type counted struct {
	n      atomic.Int64
	refuse error
}

func (c *counted) Send(context.Context, contracts.Message) error {
	c.n.Add(1)
	return c.refuse
}

// fixtures is jobs.TenantLister for the one tenant these cases write rows for.
type fixtures []tenancy.Tenant

func (f fixtures) List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) { return f, nil }

// TestARolledBackMailDecisionOwesNothing is the acceptance case the whole design
// exists for: the intent is written in the caller's transaction, so a transaction
// that aborts leaves nothing for a worker to find and nobody gets mailed.
func TestARolledBackMailDecisionOwesNothing(t *testing.T) {
	conn := intentSchema(t)
	relay := &counted{}
	svc := NewService(everybody{}, WithMailer(relay))
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	ctx := tenancy.WithTenant(t.Context(), tenant)

	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Notify(ctx, tx, contracts.Notice{
			Recipient: uuid.New(), Title: "Reset your password", Link: "/auth/reset", Wants: contracts.WantsEmail,
		})
		if err != nil {
			return err
		}
		if err := SendMail(svc, somewhere{}, true).Handler(ctx, tx, mailRequest(row.ID, row.RecipientID, uuid.New())); err != nil {
			return err
		}
		var owed int64
		if err := tx.DB().Raw("SELECT count(*) FROM " + intentTable).Scan(&owed).Error; err != nil {
			return err
		}
		if owed != 1 {
			t.Errorf("inside its own transaction the decision wrote %d intents, want 1", owed)
		}
		return errRolledBack
	})
	if !errors.Is(err, errRolledBack) {
		t.Fatalf("the case's transaction: %v", err)
	}
	if err := Deliver(svc, fixtures{tenant}, 0, 0, 0).Run(ctx, conn); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if got := relay.n.Load(); got != 0 {
		t.Errorf("a rolled-back decision sent %d messages, want none", got)
	}
}

// TestACommittedIntentSurvivesRestartAndSends is the other half: a service that never
// saw the decision sends it, because the debt is in the database and not in a queue
// this process holds.
func TestACommittedIntentSurvivesRestartAndSends(t *testing.T) {
	conn := intentSchema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	ctx := tenancy.WithTenant(t.Context(), tenant)
	cause := uuid.New()

	before := NewService(everybody{}, WithMailer(&counted{}))
	var noticeID, recipient uuid.UUID
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := before.Notify(ctx, tx, contracts.Notice{
			Recipient: uuid.New(), Title: "A task was assigned to you", Wants: contracts.WantsEmail,
		})
		if err != nil {
			return err
		}
		noticeID, recipient = row.ID, row.RecipientID
		return SendMail(before, somewhere{}, true).Handler(ctx, tx, mailRequest(noticeID, recipient, cause))
	}); err != nil {
		t.Fatalf("queue the mail: %v", err)
	}

	relay := &counted{}
	after := NewService(everybody{}, WithMailer(relay))
	if err := Deliver(after, fixtures{tenant}, 0, 0, 0).Run(ctx, conn); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if got := relay.n.Load(); got != 1 {
		t.Fatalf("a restarted worker sent %d messages, want exactly 1", got)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var state string
		var attempt int
		var reason string
		if err := tx.DB().Raw("SELECT state, attempt, reason FROM "+intentTable).Row().Scan(&state, &attempt, &reason); err != nil {
			return err
		}
		if state != contracts.MailIntentSent || attempt != 1 {
			t.Errorf("the sent intent is %s at attempt %d, want sent at attempt 1", state, attempt)
		}
		if reason != "" {
			t.Errorf("a sent intent carries the reason %q, want none", reason)
		}
		var outcome string
		err := tx.DB().Raw("SELECT outcome FROM notification_deliveries WHERE notification_id = ? AND channel = 'email' AND outcome <> 'requested'", noticeID).
			Row().Scan(&outcome)
		if err != nil {
			return err
		}
		if outcome != OutcomeSent {
			t.Errorf("the notice's email channel finished %s, want sent", outcome)
		}
		return nil
	}); err != nil {
		t.Fatalf("read the outcome: %v", err)
	}
}

// TestTheSameMailRequestWrittenTwiceOwesOneIntent is the acceptance case the unique
// partial index answers: the outbox redelivered the event, and a person is owed one
// mail and not two.
func TestTheSameMailRequestWrittenTwiceOwesOneIntent(t *testing.T) {
	conn := intentSchema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	ctx := tenancy.WithTenant(t.Context(), tenant)
	svc := NewService(everybody{}, WithMailer(&counted{}))
	cause := uuid.New()

	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return nil
	}); err != nil {
		t.Fatalf("open the schema: %v", err)
	}
	var noticeID, recipient uuid.UUID
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: uuid.New(), Title: "twice", Wants: contracts.WantsEmail})
		if err != nil {
			return err
		}
		noticeID, recipient = row.ID, row.RecipientID
		return nil
	}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	sub := SendMail(svc, somewhere{}, true)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, _, err := svc.Queue(ctx, tx, contracts.OutgoingMail{
			Purpose: contracts.EventEmailRequested, Cause: cause.String(), Person: recipient,
			Notice: noticeID, Message: contracts.Message{To: "ada@acme.example.com", Subject: "twice", Body: "the notice"},
		})
		return err
	}); err != nil {
		t.Fatalf("queue: %v", err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for i := 0; i < 2; i++ {
			if err := sub.Handler(ctx, tx, mailRequest(noticeID, recipient, cause)); err != nil {
				return err
			}
		}
		var owed int64
		if err := tx.DB().Raw("SELECT count(*) FROM " + intentTable).Scan(&owed).Error; err != nil {
			return err
		}
		if owed != 1 {
			t.Errorf("the same mail request wrote %d intents, want 1", owed)
		}
		return nil
	}); err != nil {
		t.Fatalf("redeliver: %v", err)
	}
}

// mailRequest is the event as the outbox hands it over: two identifiers and the id
// the redelivery keeps.
func mailRequest(noticeID, recipient, cause uuid.UUID) events.Event {
	payload := `{"notificationId":"` + noticeID.String() + `","recipientId":"` + recipient.String() + `"}`
	return events.Event{ID: cause, Name: contracts.EventEmailRequested, Payload: []byte(payload)}
}

var errRolledBack = errors.New("rolled back on purpose")

// The collaborators these cases need: a directory in which everybody has an address,
// and a host to build a link on.
type everybody struct{}

func (everybody) Email(context.Context, db.Tx[db.Tenant], uuid.UUID) (string, error) {
	return "ada@acme.example.com", nil
}

type somewhere struct{}

func (somewhere) PublicHost(context.Context, db.Tx[db.Tenant]) (string, error) {
	return "acme.example.com", nil
}

var _ jobs.TenantLister = fixtures(nil)
