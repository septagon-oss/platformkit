package events

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
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
// the column is null for everything nobody asked for.
type row struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	Name        string
	Payload     []byte
	CreatedAt   time.Time
	Actor       *uuid.UUID
	TraceParent *string
	TraceState  *string
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
	return RelayApp(ctx, conn, t, "")
}

// RelayApp is Relay for a composition that names its app: it claims only the rows
// whose tenant belongs to that app.
//
// The claim is the boundary, and the reason it has to be here rather than at the
// subject is what a relay without it does: it publishes another app's row at that
// app's own address, the row gets stamped, and the work is gone from this process's
// view while the app that owns it never learns there was any. The address carries
// the app, so the delivery side refuses such a message — which is the right refusal
// and the wrong place: it is made after the row was published and stamped by someone
// who had no business with it.
//
// The tenant is joined, not required. A row whose tenant row is gone names an app
// nothing declared, so it belongs to the deployment of one app (which is what every
// such row was written under until this argument existed) and to no app that names
// itself — see coalesce below.
func RelayApp(ctx context.Context, conn *db.Conn, t Transport, app appname.Name) error {
	for {
		n, err := relayBatch(ctx, conn, t, app)
		if err != nil || n < batch {
			return err
		}
	}
}

// relayBatch moves at most batch rows and reports how many it moved.
//
// The publish happens before the stamp, so a crash in between redelivers rather
// than loses — see the package comment on idempotency.
func relayBatch(ctx context.Context, conn *db.Conn, t Transport, app appname.Name) (int, error) {
	var moved int
	err := db.RunSystem(ctx, conn, relayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		var rows []row
		// The two aliases are GORM's naming rather than the columns': it maps a
		// scanned struct field by snake_case, so TraceParent arrives as
		// trace_parent. Naming the alias after the column and letting the field
		// go nil is how a carried fact silently stops being carried.
		// OF o, and that is the point of the join: without it FOR UPDATE locks the
		// rows of the one table named, which is the outbox today and would be the
		// tenant table tomorrow — and locking the tenant table from a relay would put
		// every host resolution in the country behind a batch of events.
		const q = `SELECT o.id, o.tenant_id, o.name, o.payload, o.created_at, o.actor,
			o.traceparent AS trace_parent, o.tracestate AS trace_state FROM ` + table + ` o
			LEFT JOIN tenants tn ON tn.id = o.tenant_id
			WHERE o.published_at IS NULL AND coalesce(tn.app, '') = ?
			ORDER BY o.created_at, o.id LIMIT ? FOR UPDATE OF o SKIP LOCKED`
		if err := tx.DB().Raw(q, app.String(), batch).Scan(&rows).Error; err != nil {
			return fmt.Errorf("events: relay: read the outbox: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			ev := Event{ID: r.ID, Name: r.Name, TenantID: r.TenantID, Payload: r.Payload, At: r.CreatedAt}
			if r.Actor != nil {
				ev.Actor = *r.Actor
			}
			// The trace the request left in the row, carried onto the envelope
			// the relay publishes. This transaction has no request of its own
			// to substitute: a relay span started here would join the delivery
			// to the wrong trace.
			if r.TraceParent != nil {
				ev.TraceParent = *r.TraceParent
			}
			if r.TraceState != nil {
				ev.TraceState = *r.TraceState
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
	return moved, err
}

// Purge removes published outbox history after a week. Unpublished rows remain
// recoverable regardless of age. An old handling claim remains while either its
// outbox row or terminal failure record exists: losing that claim would replay
// completed work when the pending row is relayed. Dead letters and their claims
// require explicit operator review; they are never automatically purged.
//
// A dead letter keeps its outbox row with it. The row is the only place the
// payload survives — the dead letter records the failure, not the body — so a
// purge that took the row would leave an operator reviewing a terminal failure
// whose payload is gone and whose replay answers that there is nothing to
// replay. That is the write that takes the last copy away, and a dead letter is
// still referring to the row when it happens, so the row stays. The cost is
// bounded and worth naming: an unread dead letter keeps one JSONB row alive
// past the window, and clearing it is the operator's verb — `events.Replay` or
// an operator who decides the event will never run again deletes the dead
// letter, after which the next purge takes the row.
//
// The database clock supplies the cutoff for all workers.
func Purge(ctx context.Context, conn *db.Conn) error {
	return db.RunSystem(ctx, conn, purgeToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		age := fmt.Sprintf("%d seconds", int(delivery.Keep.Seconds()))
		if err := tx.DB().Exec("DELETE FROM "+table+" o WHERE published_at IS NOT NULL AND published_at < now() - ?::interval"+
			" AND NOT EXISTS (SELECT 1 FROM "+deadLetters+" d WHERE d.event_id = o.id)", age).Error; err != nil {
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
