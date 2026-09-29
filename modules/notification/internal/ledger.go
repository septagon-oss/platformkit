package internal

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// The channels and outcomes of the delivery ledger (migrations/000027). They are
// the table's CHECK constraints spelled once in Go, so a value the table would
// refuse cannot be written from here.
const (
	ChannelInApp = "in_app"
	ChannelEmail = "email"

	OutcomeRequested  = "requested"
	OutcomeSent       = "sent"
	OutcomeSuppressed = "suppressed"
)

// record appends one row to the delivery ledger, in the transaction of the step it
// describes: the row commits with that step or not at all, so the ledger can never
// say a mail was sent by a transaction that rolled back.
func record(tx db.Tx[db.Tenant], notification uuid.UUID, channel, outcome, reason string) error {
	err := tx.DB().Exec(
		`INSERT INTO notification_deliveries (tenant_id, notification_id, channel, outcome, reason) VALUES (?, ?, ?, ?, ?)`,
		db.TenantOf(tx).ID, notification, channel, outcome, reason).Error
	if err != nil {
		return fmt.Errorf("notification: record %s %s for %s: %w", channel, outcome, notification, err)
	}
	return nil
}

// Delivery is one row of the ledger, as a caller reads it back.
type Delivery struct {
	Channel string
	Outcome string
	Reason  string
}

// Deliveries is the ledger of one notice, in the order it was appended.
func Deliveries(tx db.Tx[db.Tenant], notification uuid.UUID) ([]Delivery, error) {
	var out []Delivery
	err := tx.DB().Raw(
		`SELECT channel, outcome, reason FROM notification_deliveries WHERE notification_id = ? ORDER BY seq`,
		notification).Scan(&out).Error
	return out, err
}

// Coverage is delivery_ledger_coverage for the tenant of tx: the channels notices
// asked for, and how many of them have reached a terminal row. A requested channel
// with no terminal row is a delivery nobody can account for — still in flight, or
// lost — and the ratio is what the register (0052) measures this pillar by.
func Coverage(tx db.Tx[db.Tenant]) (requested, terminal int64, err error) {
	err = tx.DB().Raw(`
		SELECT count(*),
			count(*) FILTER (WHERE EXISTS (
				SELECT 1 FROM notification_deliveries t
				WHERE t.notification_id = r.notification_id AND t.channel = r.channel
					AND t.outcome IN ('sent', 'suppressed', 'failed')))
		FROM notification_deliveries r WHERE r.outcome = 'requested'`).Row().Scan(&requested, &terminal)
	return requested, terminal, err
}
