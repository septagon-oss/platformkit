package internal

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
)

// Fanout is the module's half of the outbox: one event in, one queued row per
// endpoint of this tenant that asked for that event type.
//
// It is a kit/events handler, mounted as the one subscription of a
// SubscribeAll manifest, so the kernel expands it into one subscription per
// event the composition emits and a module composed after this one is delivered
// by having emitted an event. That is also its exactly-once guarantee: the
// claim row kit/events writes per (event, durable) serializes the two deliveries
// of one event, and the partial unique index on (event_id, endpoint_id) WHERE
// cause='initial' means a handler that ran twice — an outbox row an operator
// replayed, a redelivery after a slow commit — cannot queue a second attempt.
//
// It performs no network call, and that is the design and not an oversight. A
// Handler runs inside a tenant transaction bounded by the consumer's own
// timeout; waiting on a customer's web server there would stall this platform's
// outbox for the length of somebody else's patience, and the three-day ladder
// this module promises cannot be expressed on the broker's. So this writes rows
// and internal.Deliver opens sockets.
//
// The payload is the CloudEvent as published — ev.MarshalJSON(), the same
// document the broker carried — because a subscriber that wanted a narrower
// shape would be a second API this module had to keep stable.
//
// Silence is not an error: an event no endpoint wants is the ordinary case, and
// returning nil is what keeps a tenant with no subscriptions out of the dead
// letter queue.
func (s *Service) Fanout(_ context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
	body, err := ev.MarshalJSON()
	if err != nil {
		return fmt.Errorf("webhook: marshal %s: %w", ev.Name, err)
	}
	// available_at is the module's clock, the same one the drain asks — see the
	// note in deliver.go. A row made due by one clock and judged by another is an
	// event that waits for a drift nobody wrote down.
	//
	// The endpoints are read to the end and the cursor closed *before* the first
	// insert. A handler holds one transaction, and a driver answers a second
	// statement issued on an open cursor with a bad connection — which is the kind
	// of defect that reads like a database fault and is a loop's fault.
	var ids []uuid.UUID
	if err := tx.DB().Raw("SELECT id FROM "+endpointTable+
		" WHERE status = 'enabled' AND deleted_at IS NULL AND events @> ARRAY[?]::text[]"+
		" ORDER BY created_at, id", ev.Name).Scan(&ids).Error; err != nil {
		return fmt.Errorf("webhook: find the endpoints of %s: %w", ev.Name, err)
	}
	tenant := db.TenantOf(tx).ID
	for _, id := range ids {
		// One statement per endpoint, each with the same payload: the fan-out is a
		// handful of rows at most, and an INSERT ... SELECT that could not report
		// which endpoints matched would be the cleverer, worse version.
		res := tx.DB().Exec("INSERT INTO "+deliveryTable+
			" (tenant_id, endpoint_id, event_id, event_name, payload, attempt, cause, state, available_at)"+
			" VALUES (?, ?, ?, ?, ?::jsonb, 1, 'initial', 'queued', ?)"+
			" ON CONFLICT (event_id, endpoint_id) WHERE cause = 'initial' DO NOTHING",
			tenant, id, ev.ID, ev.Name, string(body), s.now())
		if res.Error != nil {
			return fmt.Errorf("webhook: queue %s for endpoint %s: %w", ev.Name, id, res.Error)
		}
	}
	return nil
}
