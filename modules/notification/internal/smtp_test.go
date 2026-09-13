package internal

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// TestAMailerThatFailsFailsTheSubscription is the other half, and it is the
// half the dead letter needs: SendMail must hand the mailer's error back rather
// than swallow it, because an error is the only thing the outbox's ladder can
// count. What the ladder then does with it — four redeliveries and a row in
// platformkit_dead_letters — is kit/events' promise, and kit/events'
// TestAPoisonEventIsDeadLetteredAndStopsComingBack proves it in milliseconds,
// where the ladder's constants live.
func TestAMailerThatFailsFailsTheSubscription(t *testing.T) {
	_, conn := dbtest.Schema(t)
	broken := errors.New("notification: dial mail.acme.example.com:587: i/o timeout")
	sub := SendMail(refusing{broken}, everybody{}, somewhere{}, true)
	svc := NewService(everybody{})
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Notify(ctx, tx, contracts.Notice{
			Recipient: uuid.New(), Title: "Reset your password", Link: "/auth/reset", Email: true,
		})
		if err != nil {
			return err
		}
		payload, err := json.Marshal(contracts.EmailRequested{NotificationID: row.ID, Recipient: row.RecipientID})
		if err != nil {
			return err
		}
		handled := sub.Handler(ctx, tx, events.Event{
			ID: uuid.New(), Name: contracts.EventEmailRequested, Payload: payload,
		})
		if handled == nil {
			t.Error("a send that failed was reported as handled, so the outbox would never retry it")
		} else if !errors.Is(handled, broken) {
			t.Errorf("the subscription reported %v, want the mailer's own failure", handled)
		}
		return errRolledBack
	})
	if !errors.Is(err, errRolledBack) {
		t.Fatalf("the case's transaction: %v", err)
	}
}

var errRolledBack = errors.New("rolled back on purpose")

// The three collaborators the second case needs: a mailer that always refuses,
// a directory in which everybody has an address, and a host to build a link on.
type refusing struct{ err error }

func (r refusing) Send(context.Context, contracts.Message) error { return r.err }

type everybody struct{}

func (everybody) Email(context.Context, db.Tx[db.Tenant], uuid.UUID) (string, error) {
	return "ada@acme.example.com", nil
}

type somewhere struct{}

func (somewhere) PublicHost(context.Context, db.Tx[db.Tenant]) (string, error) {
	return "acme.example.com", nil
}
