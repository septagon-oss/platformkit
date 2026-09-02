package events

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
)

// batch bounds one relay pass. Small enough that a pass is short and its rows
// are locked briefly, large enough that a burst drains in a few ticks.
const batch = 100

// keep is how long a published row survives, for replay and for answering
// "did that event go out?". After that the purge removes it.
const keep = 7 * 24 * time.Hour

// The relay and the purge read every tenant's rows, which is the whole reason
// the capability exists; both reasons are logged wherever a system transaction
// opens.
var (
	relayToken = syscap.NewSystemToken("outbox relay")
	purgeToken = syscap.NewSystemToken("outbox purge")
)

// row is one outbox record as the relay reads it.
type row struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Name      string
	Payload   []byte
	CreatedAt time.Time
}

// Relay moves one batch of unpublished rows to the transport. kit/jobs calls it
// every second in the worker role; it is one pass so that the scheduler, and
// not this function, owns the tick and the leader election.
//
// FOR UPDATE SKIP LOCKED is what lets several workers run it at once without
// any of them waiting: each takes rows nobody else holds. The publish happens
// before the stamp, so a crash in between redelivers rather than loses — see
// the package comment on idempotency.
func Relay(ctx context.Context, conn *db.Conn, t Transport) error {
	return db.RunSystem(ctx, conn, relayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		var rows []row
		const q = `SELECT id, tenant_id, name, payload, created_at FROM ` + table + `
			WHERE published_at IS NULL ORDER BY created_at, id LIMIT ? FOR UPDATE SKIP LOCKED`
		if err := tx.DB().Raw(q, batch).Scan(&rows).Error; err != nil {
			return fmt.Errorf("events: relay: read the outbox: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			ev := Event{ID: r.ID, Name: r.Name, TenantID: r.TenantID, Payload: r.Payload, At: r.CreatedAt}
			if err := t.Publish(ctx, ev); err != nil {
				// The rows published so far are still unstamped, so they go
				// again next tick. That is the at-least-once bargain.
				return fmt.Errorf("events: relay: publish %s: %w", r.Name, err)
			}
			ids = append(ids, r.ID)
		}
		if err := tx.DB().Exec("UPDATE "+table+" SET published_at = now() WHERE id IN ?", ids).Error; err != nil {
			return fmt.Errorf("events: relay: stamp %d row(s): %w", len(ids), err)
		}
		return nil
	})
}

// Purge deletes published rows older than a week, and the handled marks of the
// same age. kit/jobs calls it hourly in the worker role. Unpublished rows are
// never touched, however old: a row that has not gone out is a queue entry, not
// history.
//
// The two windows are one window on purpose. A mark exists to recognise a
// redelivery of its own event, and an event whose outbox row is gone cannot be
// relayed again, so a mark older than the row it guards guards nothing. The
// exact residue is an event a transport still holds unacknowledged a week after
// the outbox forgot it, which JetStream's own limits make a deployment choice
// rather than a possibility this code can rule out.
func Purge(ctx context.Context, conn *db.Conn) error {
	return db.RunSystem(ctx, conn, purgeToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		before := time.Now().Add(-keep)
		if err := tx.DB().Exec("DELETE FROM "+table+" WHERE published_at IS NOT NULL AND published_at < ?", before).Error; err != nil {
			return fmt.Errorf("events: purge: %w", err)
		}
		if err := tx.DB().Exec("DELETE FROM "+handled+" WHERE handled_at < ?", before).Error; err != nil {
			return fmt.Errorf("events: purge the handled marks: %w", err)
		}
		return nil
	})
}
