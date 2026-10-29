package internal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// intentTable is the one name this package spells for 000076.
const intentTable = "notification_mail_intents"

// intentColumns is the read list, aliased to the field names contracts.MailIntent
// carries: the table keeps person_id and notification_id because they name a row of
// another table, and the contract says what they are.
const intentColumns = "id, seq, purpose, cause, person_id AS person, notification_id AS notice," +
	" recipient, subject, body, html, lang, reply_to, state, attempt, uncertain," +
	" available_at, attempted_at, reason, created_at, updated_at"

// Queue writes the mail the caller decided on into this tenant's owed list, in the
// caller's transaction, and never opens one of its own: a duty that outlived the
// decision that owed it would be a duty nobody asked for, and the acceptance case
// this file exists for is that a rolled-back decision sends nothing.
//
// contracts.PrepareIntent runs first, so the fake and Postgres refuse the same input.
// The write is one statement: ON CONFLICT DO NOTHING on the unique partial index of
// 000076, then the row is read back. A redelivered event therefore gets the intent
// that is already owed, with false for created, and two concurrent writers cannot
// both add a mail — the database decides, which is the only answer a check-then-insert
// is not.
func (s *Service) Queue(_ context.Context, tx db.Tx[db.Tenant], mail contracts.OutgoingMail) (*contracts.MailIntent, bool, error) {
	prepared, err := contracts.PrepareIntent(mail)
	if err != nil {
		return nil, false, err
	}
	var id uuid.UUID
	// available_at is the module's clock and not the column's default: the same
	// clock writes the ladder's rungs and the drain judges them due, and a row made
	// due by one clock and judged by the other is a send that waits for a drift
	// nobody intended. created_at and updated_at stay the database's.
	err = tx.DB().Raw("INSERT INTO "+intentTable+
		" (tenant_id, purpose, cause, person_id, notification_id, recipient, subject, body, html, lang, reply_to, state, available_at)"+
		" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"+
		" ON CONFLICT (tenant_id, purpose, cause) WHERE cause <> '' DO NOTHING RETURNING id",
		db.TenantOf(tx).ID, prepared.Purpose, prepared.Cause, prepared.Person, prepared.Notice,
		prepared.Message.To, prepared.Message.Subject, prepared.Message.Body, prepared.Message.HTML,
		prepared.Message.Lang, prepared.Message.ReplyTo, contracts.MailIntentQueued, s.clock()).Row().Scan(&id)
	switch {
	case err == nil:
		out, err := readIntent(tx, "id = ?", id)
		return out, true, err
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, gorm.ErrRecordNotFound):
		// The (purpose, cause) row already exists. Only a caller that named a cause
		// can reach here, because the index the conflict came from excludes the
		// empty one; anything else means this statement was refused by something
		// other than its own index, and that is said rather than answered with
		// somebody else's row.
		if prepared.Cause == "" {
			return nil, false, fmt.Errorf("notification: the mail intent for %s was not written and named no cause: %w", prepared.Purpose, err)
		}
		out, err := readIntent(tx, "purpose = ? AND cause = ?", prepared.Purpose, prepared.Cause)
		return out, false, err
	default:
		return nil, false, fmt.Errorf("notification: queue the mail intent for %s: %w", prepared.Purpose, err)
	}
}

// readIntent is the one read of a single intent, by one where-clause over this
// tenant's rows: row level security is what bounds whose intent can be read, so no
// statement here names tenant_id and a foreign id answers crud.ErrNotFound.
func readIntent(tx db.Tx[db.Tenant], where string, args ...any) (*contracts.MailIntent, error) {
	var out []contracts.MailIntent
	err := tx.DB().Raw("SELECT "+intentColumns+" FROM "+intentTable+" WHERE "+where, args...).Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("notification: read the mail intent: %w", err)
	}
	if len(out) == 0 {
		return nil, crud.ErrNotFound
	}
	return &out[0], nil
}
