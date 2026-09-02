// Package events is the one door a module's events leave through.
//
// A module writes an event with Publish, inside the same transaction as the
// state change that caused it, so the row and the change commit together or
// not at all: there is no window in which the state moved and the event was
// lost. The relay in the worker role reads those rows and hands them to a
// transport — in-process for a single-process run, JetStream for a fleet.
//
// Delivery is at-least-once, and Consume is what turns that into exactly-once
// handling: it claims each (event, subscription) pair in platformkit_handled
// inside the handler's own transaction, so a redelivery of work already done
// finds the claim taken and skips the handler.
//
// This is also the job queue: durable, retried, transactional background work
// is what an outbox is, and asking for it twice buys nothing. Periodic work is
// kit/jobs. Both arguments are docs/adr/0004.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The two tables, each named once here and once in migrations/.
const (
	table   = "platformkit_outbox"  // 000002
	handled = "platformkit_handled" // 000003
)

// Event is one thing that happened in one tenant.
type Event struct {
	// ID is the deduplication key. A handler that has already seen it has
	// already done the work; see the package comment.
	ID uuid.UUID `json:"id"`
	// Name is "<module>.<something>", the module's namespace first.
	Name string `json:"name"`
	// TenantID is the tenant the event happened in. Consume opens the
	// handler's transaction in it.
	TenantID uuid.UUID `json:"tenantId"`
	// Payload is whatever the publisher marshalled.
	Payload json.RawMessage `json:"payload"`
	// At is when the outbox row was written, which is when the state changed.
	At time.Time `json:"at"`
}

// eventName is the grammar of an event name: the module's name, a dot, and a
// lower-case path. kit/module checks a manifest's Events with ValidName, so the
// grammar exists once and a name that passes review is a name Publish accepts.
var eventName = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// ValidName reports whether name is a well-formed event name.
func ValidName(name string) bool { return eventName.MatchString(name) }

// Publish writes an event into the outbox inside tx. It is not a network call
// and it cannot fail because a broker is down: the row commits with the state
// change and the relay carries it from there.
func Publish(tx db.Tx[db.Tenant], name string, payload any) error {
	if !ValidName(name) {
		return fmt.Errorf("events: %q is not %q", name, "<module>.<event>")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("events: %s: marshal the payload: %w", name, err)
	}
	// The tenant comes from the transaction, never from the caller: an event
	// belongs to the tenant whose data changed, by construction.
	if err := tx.DB().Exec(
		"INSERT INTO "+table+" (id, tenant_id, name, payload) VALUES (?, ?, ?, ?::jsonb)",
		uuid.New(), db.TenantOf(tx).ID, name, string(body),
	).Error; err != nil {
		return fmt.Errorf("events: %s: %w", name, err)
	}
	return nil
}

// Handler is what a module does with an event. It runs inside a transaction
// scoped to the event's tenant, so anything it writes commits with the
// acknowledgement of the event and rolls back with a redelivery.
type Handler func(ctx context.Context, tx db.Tx[db.Tenant], ev Event) error

// Transport carries events between processes. There are two implementations
// and no third: Memory for a single-process run and its tests, JetStream for a
// fleet. Both deliver at least once.
type Transport interface {
	Publish(ctx context.Context, ev Event) error
	// Subscribe delivers every event called name to h until ctx is done.
	// durable names the subscription so a consumer that restarts resumes where
	// it stopped rather than replaying from the beginning; an error from h is a
	// negative acknowledgement and the event comes back.
	Subscribe(ctx context.Context, durable, name string, h func(ctx context.Context, ev Event) error) error
}

// Subscription is one module's interest in one event. A module lists its
// subscriptions in its manifest; kit/app refuses to start when one names an
// event no module publishes.
type Subscription struct {
	Module  string
	Name    string
	Handler Handler
}

// durable is the subscription's name on the transport. Dots separate a subject,
// so they cannot appear in a consumer name: the two halves join with a dash.
func (s Subscription) durable() string {
	return s.Module + "-" + strings.ReplaceAll(s.Name, ".", "-")
}

// Consume subscribes every handler in subs to its event. Each delivery opens a
// transaction in the event's own tenant, so a handler reaches the tenant's rows
// the same way a request handler does and can publish events of its own into
// the same transaction.
//
// Each delivery is claimed before the handler runs, so a handler sees each
// event once however many times the transport delivers it. See claim.
func Consume(ctx context.Context, conn *db.Conn, t Transport, subs []Subscription) error {
	for _, s := range subs {
		if s.Handler == nil {
			return fmt.Errorf("events: subscription %s to %s has no handler", s.Module, s.Name)
		}
		h, durable := s.Handler, s.durable()
		deliver := func(ctx context.Context, ev Event) error {
			// Only the id is known here. It is all kit/db needs to scope the
			// transaction, and it is what row-level security reads.
			ctx = tenancy.WithTenant(ctx, tenancy.Tenant{ID: ev.TenantID})
			return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				first, err := claim(tx, ev.ID, durable)
				if err != nil || !first {
					return err
				}
				return h(ctx, tx, ev)
			})
		}
		if err := t.Subscribe(ctx, durable, s.Name, deliver); err != nil {
			return fmt.Errorf("events: subscribe %s to %s: %w", s.Module, s.Name, err)
		}
	}
	return nil
}

// claim writes this subscription's mark against the event and reports whether
// it was the one that wrote it. A second delivery conflicts on the primary key,
// inserts nothing, and is told to skip the handler.
//
// It runs inside the handler's own transaction, which is the whole design: the
// mark and everything the handler writes commit together, so a handler that
// fails rolls its claim back with its work and sees the event again, and a
// handler that succeeded can never run twice. A separate transaction would
// leave a window between the two in which a crash loses one or repeats the
// other, which is the problem this exists to remove.
func claim(tx db.Tx[db.Tenant], id uuid.UUID, durable string) (bool, error) {
	res := tx.DB().Exec("INSERT INTO "+handled+" (event_id, durable, tenant_id) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
		id, durable, db.TenantOf(tx).ID)
	if res.Error != nil {
		return false, fmt.Errorf("events: claim %s for %s: %w", id, durable, res.Error)
	}
	return res.RowsAffected == 1, nil
}
