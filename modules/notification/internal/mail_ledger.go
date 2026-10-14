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
// answer to it is the same word the neutral acknowledgment already uses — and why
// a stranger must not be answered with it alone: "no row for that id" is the shape
// of "nobody has this address" — which is why the read takes an id and why no
// method here answers "what is the transport doing" out of the tenant's newest
// row (contracts.MailLedger, contracts.MailReport).
//
// This lookup is by request id alone. An empty requestID is refused rather than
// answered with somebody else's row. RLS is what makes another tenant's id
// answer known=false rather than refuse differently.
//
// The kinds it is handed narrow the rows it may answer from, and nothing else:
// they are bound as parameters, the order and the LIMIT are the same, and a kind
// the caller does not name is a kind this read cannot see. Answering "no row of
// those kinds" and "no row at all" with the same known=false is the whole point —
// a caller that asks among some kinds is a caller that has nothing to say about
// the others, and it must not be able to tell them apart (contracts.MailReport).
func (s *Service) MailOutcome(_ context.Context, tx db.Tx[db.Tenant], requestID string, kinds ...string) (string, bool, error) {
	if requestID == "" {
		return "", false, contracts.ErrMailRequest
	}
	query, args := mailOutcomeQuery(requestID, kinds)
	var outcome string
	err := tx.DB().Raw(query, args...).Row().Scan(&outcome)
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, gorm.ErrRecordNotFound):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("notification: read the mail delivery of request %s: %w", requestID, err)
	}
	return outcome, true, nil
}

// mailOutcomeQuery is the read with its parameters in one list: the request id and,
// when the caller named kinds, the list as one `IN ?` argument the way the rest of
// this codebase asks for a set (kit/events/relay.go, modules/file). No id, address
// or kind is ever written into the SQL.
func mailOutcomeQuery(requestID string, kinds []string) (string, []any) {
	query := "SELECT outcome FROM direct_mail_deliveries WHERE request_id = ?"
	args := []any{requestID}
	if len(kinds) > 0 {
		query += " AND kind IN ?"
		args = append(args, kinds)
	}
	return query + " ORDER BY seq DESC LIMIT 1", args
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
