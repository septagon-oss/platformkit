package internal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// RecordMail appends one row to the record of direct mails, in the transaction of
// the send it describes, for the reason `record` already gives for the ledger: the
// row commits with that step or not at all, so nothing here can say a mail was
// sent by a transaction that rolled back.
//
// A direct mail is not a channel of a notice, which is why this is a second table
// and a second method rather than a row in notification_deliveries — see
// contracts.MailLedger and migrations/000044. contracts.PrepareMail runs first, so
// the record refuses what the table's CHECKs refuse, and the fake refuses it too.
func (s *Service) RecordMail(_ context.Context, tx db.Tx[db.Tenant], r contracts.MailRecord) error {
	prepared, err := contracts.PrepareMail(r)
	if err != nil {
		return err
	}
	err = tx.DB().Exec(
		`INSERT INTO direct_mail_deliveries (tenant_id, recipient, kind, outcome, reason, traceparent, request_id)`+
			` VALUES (?, ?, ?, ?, ?, ?, ?)`,
		db.TenantOf(tx).ID, prepared.Recipient, prepared.Kind, prepared.Outcome, prepared.Reason,
		nullIfEmpty(prepared.Traceparent), nullIfEmpty(prepared.RequestID)).Error
	if err != nil {
		return fmt.Errorf("notification: record the %s mail to %s: %w", prepared.Outcome, prepared.Recipient, err)
	}
	return nil
}

// MailOutcome is the newest outcome recorded for one request id, and known=false
// when that request left no record. Every request that asked for no mail gets that
// answer, which is why "no row" is not news to a caller and why the caller's
// answer to it is the same word the neutral acknowledgment already uses.
//
// The lookup is by request id alone and never by "the newest row in the tenant":
// an empty requestID is refused rather than answered, because the newest untraced
// row belongs to somebody else's call. RLS is what makes another tenant's id
// answer known=false rather than refuse differently.
func (s *Service) MailOutcome(_ context.Context, tx db.Tx[db.Tenant], requestID string) (string, bool, error) {
	if requestID == "" {
		return "", false, contracts.ErrMailRequest
	}
	var outcome string
	err := tx.DB().Raw(
		`SELECT outcome FROM direct_mail_deliveries WHERE request_id = ? ORDER BY seq DESC LIMIT 1`,
		requestID).Row().Scan(&outcome)
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, gorm.ErrRecordNotFound):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("notification: read the mail delivery of request %s: %w", requestID, err)
	}
	return outcome, true, nil
}

// nullIfEmpty stores an absent trace attribute as NULL, the way
// kit/events.Publish stores an absent one (events.go's nilIfEmpty): the columns
// are nullable, "this mail had no call behind it" is the ordinary case, and the
// only way to ask the table which rows were never traced is IS NULL.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
