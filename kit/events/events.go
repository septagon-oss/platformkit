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
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The three tables, each named once here and once in migrations/.
const (
	table       = "platformkit_outbox"       // 000002
	handled     = "platformkit_handled"      // 000003
	deadLetters = "platformkit_dead_letters" // 000005
)

// maxDeliveries bounds how many times one event is handed to one subscription
// before both transports give up on it: a poison event — one that fails for a
// reason no retry can fix — would otherwise come back forever and spend the
// worker on it. The last attempt terminates the message and writes a row to
// platformkit_dead_letters instead.
//
// backoff is the wait before each redelivery, so it is one shorter than
// maxDeliveries: the first delivery does not wait. Both are variables rather
// than constants only so that internal_test.go can run the whole ladder in
// milliseconds; nothing outside this package can see them.
var (
	maxDeliveries = 5
	backoff       = []time.Duration{time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second}
)

// deadLetterToken is the capability the dead-letter write needs. It is a system
// transaction and not the event's own tenant transaction because the reason a
// delivery failed may be that the tenant transaction could not be opened.
var deadLetterToken = syscap.NewSystemToken("record an event no subscription could handle")

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
	// The tenant comes from the transaction, never from the caller: an event
	// belongs to the tenant whose data changed, by construction.
	return write(tx.DB(), db.TenantOf(tx).ID, name, payload)
}

// PublishFor writes an event from a cross-tenant transaction, naming the tenant
// it belongs to.
//
// It exists for one shape of work and is the only place in the program where a
// tenant is an argument to an event. The control plane creates a tenant, and the
// event that says so has to be written in the transaction that created it — a
// transaction that belongs to no tenant, about a tenant that did not exist a
// statement earlier. Publish cannot express that, and publishing afterwards in
// a second transaction would be an event that can be lost while its cause is
// kept, which is the exact failure the outbox exists to remove.
//
// A caller needs a db.Tx[db.System] to reach it, so the audience is the modules
// that already hold the capability. See docs/adr/0006.
func PublishFor(tx db.Tx[db.System], tenantID uuid.UUID, name string, payload any) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("events: %s: an event belongs to a tenant", name)
	}
	return write(tx.DB(), tenantID, name, payload)
}

func write(gdb *gorm.DB, tenantID uuid.UUID, name string, payload any) error {
	if !ValidName(name) {
		return fmt.Errorf("events: %q is not %q", name, "<module>.<event>")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("events: %s: marshal the payload: %w", name, err)
	}
	if err := gdb.Exec(
		"INSERT INTO "+table+" (id, tenant_id, name, payload) VALUES (?, ?, ?, ?::jsonb)",
		uuid.New(), tenantID, name, string(body),
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
// fleet. Both deliver at least once, both give up after maxDeliveries, and both
// dead-letter what they gave up on — a transport that agreed with the other
// about everything except when to stop would be two policies, not one.
type Transport interface {
	Publish(ctx context.Context, ev Event) error
	// Subscribe delivers every event called name to sink until ctx is done.
	// durable names the subscription, so a consumer that restarts resumes where
	// it stopped rather than replaying from the beginning.
	Subscribe(ctx context.Context, durable, name string, sink Sink) error
}

// Sink is what a transport does with one event. Handle runs the handler and an
// error from it is a negative acknowledgement, so the event comes back; Dead is
// called instead once the transport has stopped bringing it back.
//
// It is a struct rather than a second parameter because the two belong to one
// subscription, and a transport that had only Handle could only choose between
// losing a poison event and retrying it forever.
type Sink struct {
	Handle func(ctx context.Context, ev Event) error
	Dead   func(ctx context.Context, ev Event, cause error)
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
		sink := Sink{
			Handle: func(ctx context.Context, ev Event) error {
				// A handler holds a transaction, so it is bounded: a handler
				// still running when the transport's acknowledgement deadline
				// passes is a handler whose event is redelivered while its
				// first attempt is still writing. handlerTimeout is shorter
				// than the JetStream AckWait for exactly that reason.
				ctx, cancel := context.WithTimeout(ctx, handlerTimeout)
				defer cancel()
				// Only the id is known here. It is all kit/db needs to scope
				// the transaction, and it is what row-level security reads.
				ctx = tenancy.WithTenant(ctx, tenancy.Tenant{ID: ev.TenantID})
				return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					first, err := claim(tx, ev.ID, durable)
					if err != nil || !first {
						return err
					}
					return h(ctx, tx, ev)
				})
			},
			Dead: func(ctx context.Context, ev Event, cause error) {
				if err := deadLetter(ctx, conn, ev, durable, cause); err != nil {
					slog.ErrorContext(ctx, "events: could not record a dead letter",
						"event", ev.Name, "id", ev.ID, "durable", durable, "error", err)
				}
			},
		}
		if err := t.Subscribe(ctx, durable, s.Name, sink); err != nil {
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
//
// The cost is a lock, and it is worth naming. An INSERT ... ON CONFLICT DO
// NOTHING against a row another open transaction has already inserted does not
// return zero rows: it blocks on that row's lock until the first transaction
// commits or rolls back, and only then learns which. So a redelivery that
// arrives while the first attempt is still running does not run the handler
// twice and does not skip it either — it waits. What bounds the wait is
// handlerTimeout: the first attempt holds its transaction for at most that
// long, so the second waits at most that long before it is told the work is
// done (commit) or given the work itself (rollback). Two deliveries of one
// event are therefore serialized rather than concurrent, and the timeout is
// what keeps "serialized" from meaning "stuck".
func claim(tx db.Tx[db.Tenant], id uuid.UUID, durable string) (bool, error) {
	res := tx.DB().Exec("INSERT INTO "+handled+" (event_id, durable, tenant_id) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
		id, durable, db.TenantOf(tx).ID)
	if res.Error != nil {
		return false, fmt.Errorf("events: claim %s for %s: %w", id, durable, res.Error)
	}
	return res.RowsAffected == 1, nil
}

// handlerTimeout bounds one delivery. It is shorter than the JetStream
// acknowledgement deadline (ackWait), because a handler that is still running
// when that passes has its event redelivered while its own transaction is still
// open. The claim in platformkit_handled turns that overlap into a wait rather
// than into two concurrent handlers — see claim — so this constant is also the
// bound on how long a redelivery blocks: a handler that runs to this timeout is
// a handler that has held every later delivery of the same event for as long.
// That is why it is a bound on the handler and not only a deadline for the
// transport.
const handlerTimeout = 25 * time.Second

// deadLetter records an event that no number of redeliveries could get handled,
// with the last error, and says so in the log. A row is an alert and not a
// queue: nothing redelivers from that table.
//
// It leaves no claim, and that has a consequence worth stating. Every attempt
// rolled its claim back with its work, so platformkit_handled holds nothing for
// this event and this subscription. Exactly-once handling is therefore a
// promise about deliveries the transport makes, not about the outbox row: if
// the row were relayed again — by an operator replaying it, or by a purge that
// had not yet reached it after the transport forgot the message — the handler
// would run again, from the top, however many times it already failed. The
// dead-letter row is what an operator reads before deciding whether that is
// what they want.
//
// ON CONFLICT DO NOTHING because a transport may hand the same exhausted event
// over more than once, and the first account of the failure is the useful one.
func deadLetter(ctx context.Context, conn *db.Conn, ev Event, durable string, cause error) error {
	slog.ErrorContext(ctx, "events: giving up on an event",
		"event", ev.Name, "id", ev.ID, "durable", durable, "attempts", maxDeliveries, "error", cause)
	// WithoutCancel: this runs on the way out of a failed delivery, and a
	// cancelled worker context would lose the only record of it.
	ctx = context.WithoutCancel(ctx)
	return db.RunSystem(ctx, conn, deadLetterToken, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec("INSERT INTO "+deadLetters+" (event_id, durable, tenant_id, name, error) VALUES (?, ?, ?, ?, ?) ON CONFLICT DO NOTHING",
			ev.ID, durable, ev.TenantID, ev.Name, cause.Error()).Error
	})
}
