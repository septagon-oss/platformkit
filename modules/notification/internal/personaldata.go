// This file is this module's answer to one contract: personaldata.Data (ADR 0020).
//
// Export is a read of the rows this module owns about one person, in the shape this
// module's own contracts use. Erase removes what those rows *say* and keeps the rows
// wherever the audit trail has to keep a row; the person's id survives, the strings
// that named them do not. Both run inside the caller's transaction, inside the tenant
// row-level security already bounded, so neither names tenant_id.
//
// It is a contributor and never a command: no code in this module calls it. The module
// that composes the parts (modules/subject) does, and the kernel reads the same
// declaration below off the manifest to refuse a column this file does not scrub.
package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
)

// personalData is what a tenant was told about a person, and what was told to them. The
// row stays: notification_deliveries reaches a recipient only through notification_id,
// so deleting a notification would silently orphan a ledger that carries an index and a
// CHECK, and the anonymised row still answers "when did we tell them". The device rows are
// the exception: they hold a credential rather than a memory, and Erase says why.
type personalData struct{}

// PersonalData is this module's contribution to a person's export and erasure.
func PersonalData() personaldata.Part {
	return personaldata.Part{Module: "notification", Data: personalData{}}
}

type notificationPart struct {
	Notifications []notificationRow `json:"notifications"`
	Deliveries    []deliveryRow     `json:"deliveries"`
	Intents       []intentRow       `json:"intents"`
	Devices       []deviceRow       `json:"devices"`
}

// intentRow is an owed mail without its message. The person's own notice already
// exports its title, body and link, so the copy the mail was going to carry would be
// handed out twice; what the mail adds that the notice does not say is whether it
// went, which is the state and the reason.
type intentRow struct {
	Purpose     string     `json:"purpose"`
	Cause       string     `json:"cause,omitempty"`
	State       string     `json:"state"`
	Attempt     int        `json:"attempt"`
	Uncertain   int        `json:"uncertain"`
	Reason      string     `json:"reason,omitempty"`
	AttemptedAt *time.Time `json:"attemptedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type notificationRow struct {
	ID        uuid.UUID  `json:"id"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Link      string     `json:"link"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

type deliveryRow struct {
	Channel   string    `json:"channel"`
	Outcome   string    `json:"outcome"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}

// deviceRow is a device without its token. The token is a bearer address for the
// person's phone or browser, and the door onto an export is a permission somebody
// other than the person can hold; a bundle that carried tokens would let that holder
// send to the person's device. Which platform, and since when, is the fact a person
// asks about; the credential is nobody's business but the carrier's.
type deviceRow struct {
	Platform  string    `json:"platform"`
	CreatedAt time.Time `json:"createdAt"`
}

func (personalData) Export(ctx context.Context, tx db.Tx[db.Tenant],
	s personaldata.Subject) (json.RawMessage, error) {
	var out notificationPart
	db_ := tx.DB().WithContext(ctx)
	err := db_.Table("notifications").
		Select("id, title, body, link, read_at, created_at").
		Where("recipient_id = ?", s.ID).Order("created_at, id").Find(&out.Notifications).Error
	if err != nil {
		return nil, fmt.Errorf("notification: export %s: %w", s.ID, err)
	}
	err = db_.Table("notification_deliveries").
		Select("channel, outcome, reason, created_at").
		Where("notification_id IN (SELECT id FROM notifications WHERE recipient_id = ?)", s.ID).
		Order("seq").Find(&out.Deliveries).Error
	if err != nil {
		return nil, fmt.Errorf("notification: export %s's deliveries: %w", s.ID, err)
	}
	err = db_.Table("notification_mail_intents").
		Select("purpose, cause, state, attempt, uncertain, reason, attempted_at, created_at").
		Where("person_id = ?", s.ID).Order("seq").Find(&out.Intents).Error
	if err != nil {
		return nil, fmt.Errorf("notification: export %s's owed mail: %w", s.ID, err)
	}
	err = db_.Table("notification_devices").
		Select("platform, created_at").
		Where("recipient_id = ? AND deleted_at IS NULL", s.ID).
		Order("created_at, id").Find(&out.Devices).Error
	if err != nil {
		return nil, fmt.Errorf("notification: export %s's devices: %w", s.ID, err)
	}
	return json.Marshal(out)
}

// Erase scrubs the three text columns of a notification and the reason column of the
// delivery ledger, over the whole tenant and not only this person's rows.
//
// That is deliberate and it is the difference between a rule and a gesture: the message
// another person received about this one carries this one's name in its title, and an
// erasure that limited itself to rows with the recipient's id would leave the sentence
// that actually repeats the address sitting in somebody else's inbox history.
func (personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	if _, err := personaldata.Scrub(ctx, tx, s, "notifications",
		[]string{"title", "body", "link"}, ""); err != nil {
		return err
	}
	_, err := personaldata.Scrub(ctx, tx, s, "notification_deliveries", []string{"reason"}, "")
	if err != nil {
		return err
	}
	// The owed mail holds the address it was owed to and the message it was going to
	// carry, so all five of its text columns go. The row keeps its state, its purpose
	// and its counters: "we were owed a mail, it was attempted twice and the relay
	// never answered" is a fact about this module's behaviour that survives the words
	// it was written in — and a scrubbed row then fails the worker's address re-ask,
	// so an erased person is never mailed at an address nobody decided for them.
	if _, err := personaldata.Scrub(ctx, tx, s, "notification_mail_intents",
		[]string{"recipient", "subject", "body", "html", "reason"}, ""); err != nil {
		return err
	}
	// A device row exists to reach the person, and its token is their device's
	// address. Soft-deleting it would keep that address findable in a table nothing
	// reads again, so the erasure is the one DELETE here: no notice can be sent to a
	// person who asked to stop being told, and a device they register again after an
	// erasure arrives as a new row, which is what the live-token index already expects.
	return tx.DB().WithContext(ctx).
		Exec("DELETE FROM notification_devices WHERE recipient_id = ?", s.ID).Error
}
