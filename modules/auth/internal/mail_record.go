package internal

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// recordMail leaves the record of one mail this module sent itself, and — when it
// did not go — says so in the trail beside it. Both happen in the caller's
// transaction, so the record and its event commit with the step they describe or
// not at all: a `sent` row whose transaction aborted is a mail nobody recorded,
// which is the direction this table would rather be wrong in.
//
// `said` is what the transport returned, and nothing else may reach the record:
// contracts.RedactMailReason strips every secret handed to it, because a transport
// may quote its input in an error and the outbox retains handler errors. A
// transport that says nothing at all still leaves a record that says why the mail
// did not go, which is the table's own rule (000044's CHECK).
//
// A composition that wires no MailLedger records nothing, and this function does
// not pretend otherwise: an unwired port is the bug this record exists to close,
// not a mode to preserve. It publishes the failure regardless, because the trail
// and the ledger are two answers and a deployment missing one of them should still
// have the other.
func (s *Service) recordMail(ctx context.Context, tx db.Tx[db.Tenant], kind, recipient, outcome, said string, secrets ...string) error {
	traceparent, requestID := originOf(ctx)
	reason := ""
	if outcome != notificationcontracts.MailSent {
		reason = notificationcontracts.RedactMailReason(said, secrets...)
		if reason == "" {
			reason = "the mail transport said nothing"
		}
	}
	address := contracts.EmailKey(recipient)
	if s.mail.Mails != nil {
		err := s.mail.Mails.RecordMail(ctx, tx, notificationcontracts.MailRecord{
			Kind: kind, Recipient: address, Outcome: outcome, Reason: reason,
			Traceparent: traceparent, RequestID: requestID,
		})
		if err != nil {
			return err
		}
	}
	if outcome != notificationcontracts.MailFailed {
		return nil
	}
	return events.Publish(ctx, tx, contracts.EventMailFailed, contracts.MailFailed{
		Kind: kind, Recipient: address, Reason: reason,
		RequestID: requestID, Traceparent: traceparent, At: db.Now(),
	})
}
