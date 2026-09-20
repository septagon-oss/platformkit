package events

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/internal/delivery"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
)

// batch bounds one relay pass. Small enough that a pass is short and its rows
// are locked briefly, large enough that a burst drains in a few ticks.
const batch = 100

// The relay and the purge read every tenant's rows, which is the whole reason
// the capability exists; both reasons are logged wherever a system transaction
// opens.
var (
	relayToken = syscap.NewSystemToken("outbox relay")
	purgeToken = syscap.NewSystemToken("outbox purge")
)

// row is one outbox record as the relay reads it. Actor is a pointer because
// the column is null for everything nobody asked for. The last two are matched by
// an explicit column name because gorm matches a field to the snake_case of its
// name, and `traceparent` is one word: without the tag the field stays empty and
// the trace context disappears on its way to the broker without a word about it.
type row struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Name      string
	Payload   []byte
	CreatedAt time.Time
	Actor     *uuid.UUID
	// The publisher's trace context, empty for everything that was not traced.
	// The relay does not read it into its own span — one batch is many
	// unrelated traces — it carries it to the envelope, where kit/events puts it
	// back on the handler's context. See trace.go.
	TraceParent string `gorm:"column:traceparent"`
	TraceState  string `gorm:"column:tracestate"`
}

// Relay moves every unpublished row to the transport, a batch at a time, and
// returns when the queue is empty or ctx is done. kit/jobs calls it every second
// in the worker role.
//
// It takes no lock, and that is deliberate: FOR UPDATE SKIP LOCKED is the
// concurrency control, so several workers relaying at once each take rows
// nobody else holds and none of them waits. An advisory lock around it would
// add nothing and would take something away — a transport that blocks inside
// one relay pass would hold that lock, and every other periodic job on every
// replica would stop behind it.
//
// One transaction per batch rather than one for the whole drain, so the row
// locks are held for a batch and not for a backlog. The caller bounds the whole
// thing with a deadline; a pass that runs out of time leaves its rows unstamped
// and the next tick takes them.
func Relay(ctx context.Context, conn *db.Conn, t Transport) error {
	for {
		n, err := relayBatch(ctx, conn, t)
		if err != nil || n < batch {
			return err
		}
	}
}

// relayBatch moves at most batch rows and reports how many it moved.
//
// The publish happens before the stamp, so a crash in between redelivers rather
// than loses — see the package comment on idempotency.
//
// One span for the pass, not one per row: what a reader wants from a relay span
// is whether the queue is draining and how long a pass took, and a burst of a
// hundred events would otherwise make the worker's own trace a hundred spans of
// queue housekeeping. A pass that published nothing still makes one, because "the
// relay runs and moves nothing" is the answer to a question somebody asked.
func relayBatch(ctx context.Context, conn *db.Conn, t Transport) (int, error) {
	ctx, span := tracer.Start(ctx, "outbox relay batch")
	defer span.End()
	var moved int
	err := db.RunSystem(ctx, conn, relayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		var rows []row
		const q = `SELECT id, tenant_id, name, payload, created_at, actor, traceparent, tracestate FROM ` + table + `
			WHERE published_at IS NULL ORDER BY created_at, id LIMIT ? FOR UPDATE SKIP LOCKED`
		if err := tx.DB().Raw(q, batch).Scan(&rows).Error; err != nil {
			return fmt.Errorf("events: relay: read the outbox: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			ev := Event{ID: r.ID, Name: r.Name, TenantID: r.TenantID, Payload: r.Payload, At: r.CreatedAt,
				TraceParent: r.TraceParent, TraceState: r.TraceState}
			if r.Actor != nil {
				ev.Actor = *r.Actor
			}
			if err := t.Publish(ctx, ev); err != nil {
				// The rows published so far are still unstamped, so they go
				// again next tick. That is the at-least-once bargain.
				return fmt.Errorf("events: relay: publish %s: %w", r.Name, err)
			}
			ids = append(ids, r.ID)
		}
		if err := tx.DB().Exec("UPDATE "+table+" SET published_at = clock_timestamp() WHERE id IN ?", ids).Error; err != nil {
			return fmt.Errorf("events: relay: stamp %d row(s): %w", len(ids), err)
		}
		moved = len(ids)
		return nil
	})
	span.SetAttributes(attribute.Int("platformkit.events.relayed", moved))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return moved, err
}

// Purge removes published outbox history after a week. Unpublished rows remain
// recoverable regardless of age. An old handling claim remains while either its
// outbox row or terminal failure record exists: losing that claim would replay
// completed work when the pending row is relayed. Dead letters and their claims
// require explicit operator review; they are never automatically purged.
// The database clock supplies the cutoff for all workers.
func Purge(ctx context.Context, conn *db.Conn) error {
	return db.RunSystem(ctx, conn, purgeToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		age := fmt.Sprintf("%d seconds", int(delivery.Keep.Seconds()))
		if err := tx.DB().Exec("DELETE FROM "+table+
			" WHERE published_at IS NOT NULL AND published_at < now() - ?::interval", age).Error; err != nil {
			return fmt.Errorf("events: purge: %w", err)
		}
		if err := tx.DB().Exec("DELETE FROM "+handled+" h WHERE handled_at < now() - ?::interval"+
			" AND NOT EXISTS (SELECT 1 FROM "+table+" o WHERE o.id = h.event_id)"+
			" AND NOT EXISTS (SELECT 1 FROM "+deadLetters+" d WHERE d.event_id = h.event_id AND d.durable = h.durable)", age).Error; err != nil {
			return fmt.Errorf("events: purge the handled marks: %w", err)
		}
		return nil
	})
}
