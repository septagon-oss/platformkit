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
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The three tables, each named once here and once in migrations/.
const (
	table       = "platformkit_outbox"       // 000002
	handled     = "platformkit_handled"      // 000003
	deadLetters = "platformkit_dead_letters" // 000005
)

// deadLetterToken is the capability the dead-letter write needs. It is a system
// transaction and not the event's own tenant transaction because the reason a
// delivery failed may be that the tenant transaction could not be opened.
var deadLetterToken = syscap.NewSystemToken("record an event no subscription could handle")

// tenantToken is the capability the delivery's app check reads with. Which app
// holds a tenant is a fact about the deployment rather than about any tenant's
// rows, and the one read that answers it is over the tenant table, which no
// tenant's own transaction is entitled to ask about another tenant's row.
var tenantToken = syscap.NewSystemToken("say which app holds an event's tenant")

// Event is the portable envelope shared by the outbox and its transports.
type Event = transport.Event

// ValidName applies the event grammar shared by manifests and transports.
func ValidName(name string) bool { return transport.ValidName(name) }

// Publish writes an event into the outbox inside tx. It is not a network call
// and it cannot fail because a broker is down: the row commits with the state
// change and the relay carries it from there.
//
// It takes a context only to read the actor off it. A db.Tx carries none — a
// transaction is a handle, not a scope somebody can cancel through — so the
// caller's own context is the one thing that knows whose request this is.
func Publish(ctx context.Context, tx db.Tx[db.Tenant], name string, payload any) error {
	// The tenant comes from the transaction, never from the caller: an event
	// belongs to the tenant whose data changed, by construction.
	return write(ctx, tx.DB(), db.TenantOf(tx).ID, name, payload)
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
func PublishFor(ctx context.Context, tx db.Tx[db.System], tenantID uuid.UUID, name string, payload any) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("events: %s: an event belongs to a tenant", name)
	}
	return write(ctx, tx.DB(), tenantID, name, payload)
}

// write is the one INSERT. The actor is whatever kit/tenancy has on the
// context and NULL otherwise, passed as an untyped nil so the column is null
// rather than the nil UUID: "nobody" and "the user 00000000-…" are different
// answers and only one of them is true.
func write(ctx context.Context, gdb *gorm.DB, tenantID uuid.UUID, name string, payload any) error {
	if !ValidName(name) {
		return fmt.Errorf("events: %q is not %q", name, "<module>.<event>")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("events: %s: marshal the payload: %w", name, err)
	}
	// The promise the emitting module made in its manifest is checked here, at
	// the one door, before anything is written. Whose promise it is, is which app
	// holds the tenant this row belongs to: one process may hold two compositions,
	// and the check academy's boot declared must not be the one acme's event is
	// measured against, nor acme's be missing when academy's own event is written.
	// The read is asked only when some app typed this event — an event no app
	// described has no contract to consult in any app — so an unchecked publish
	// still costs exactly the INSERT it always cost. See catalog.go.
	if payloadTyped(name) {
		app, err := appOfTenant(gdb, tenantID)
		if err != nil {
			return fmt.Errorf("events: %s: %w", name, err)
		}
		if err := checkPayload(app, name, body); err != nil {
			return err
		}
	}
	var actor any
	if id, ok := tenancy.ActorFrom(ctx); ok {
		actor = id
	}
	// The trace context is stored beside the actor for the same reason the actor
	// is: the relay publishes later, in a transaction of its own and with no
	// request left to ask. Storing it here is what lets a delivery name the call
	// that caused it. Absent is normal and stays absent — a periodic job, a
	// handler reacting to another event. See kit/trace and kit/events/trace.go.
	//
	// Three trace members, not two: the request id that lets an operator quote
	// this write in a trace travels beside the trace parent, in the baggage the
	// router wrote — see migrations/000041. The request id and the client address
	// arrive in the same breath and for the same reason: they are readable only
	// while the call is open, and the row outlives it. The baggage and the column
	// hold the same id by different owners — the baggage is the trace's own
	// correlation member, read back onto the delivery's context, while request_id
	// is the audit trail's answer to "which call", read by a query — and the
	// together set is what lets the trail answer who, what, when, which call and
	// from where: see kit/request and migrations/000034. An absent member is
	// written as NULL and not as the empty string: the propagator answers "" for
	// what it was not given, and a row that carries no trace is asked about with
	// `traceparent IS NULL` — the query the migration says is ordinary, and one
	// the empty string answers with an empty result set.
	parent, state, correlation := carriedContext(ctx)
	req, _ := request.From(ctx)
	if err := gdb.Exec(
		"INSERT INTO "+table+" (id, tenant_id, name, payload, actor, traceparent, tracestate, baggage, request_id, client_ip)"+
			" VALUES (?, ?, ?, ?::jsonb, ?, ?, ?, ?, ?, NULLIF(?, '')::inet)",
		uuid.New(), tenantID, name, string(body), actor,
		nilIfEmpty(parent), nilIfEmpty(state), nilIfEmpty(correlation),
		nilIfEmpty(req.ID), req.ClientAddr,
	).Error; err != nil {
		return fmt.Errorf("events: %s: %w", name, err)
	}
	return nil
}

// nilIfEmpty stores an absent value as NULL rather than the empty string: the
// envelope omits an attribute that does not apply, and the row that carries it says
// the same thing the envelope does. The propagator yields the empty string for what
// it does not hold, which is right for a carrier and wrong for a row: the columns are
// nullable, "no trace" is the ordinary case rather than a defect, and the only way a
// person can ask a table which of its rows were never traced is with IS NULL. The
// empty string would make an untraced publish look like a traced one with a corrupt
// header, and would answer that query with nothing.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Handler is what a module does with an event. It runs inside a transaction
// scoped to the event's tenant, so anything it writes commits with the
// acknowledgement of the event and rolls back with a redelivery.
type Handler func(ctx context.Context, tx db.Tx[db.Tenant], ev Event) error

// Transport carries committed events through the portable delivery contract.
type Transport = transport.Transport

// Sink handles deliveries and records terminal failures under that contract.
type Sink = transport.Sink

// Subscription is one module's interest in one event. A module lists its
// subscriptions in its manifest; kit/app refuses to start when one names an
// event no module publishes.
type Subscription struct {
	// App is the slug of the app this subscription belongs to. It goes into the
	// durable name, which is the JetStream consumer name, the deliver group and
	// half the key of the handled ledger and the dead-letter row — the one name
	// that says which app owns a delivery. Left empty, the subscription is the
	// deployment of one app and keeps the durable every consumer already has.
	App     appname.Name
	Module  string
	Name    string
	Handler Handler
}

// durable is the subscription's name on the transport. Dots separate a subject,
// so they cannot appear in a consumer name: the two halves join with a dash.
func (s Subscription) durable() string {
	return appname.Durable(s.App, s.Module, s.Name)
}

// Consume subscribes every handler in subs to its event. Each delivery opens a
// transaction in the event's own tenant, so a handler reaches the tenant's rows
// the same way a request handler does and can publish events of its own into
// the same transaction.
//
// "The event's own tenant" is a tenant this app holds, and three checks say so
// before a handler runs. The transport compares the *address* it routed by with
// the document (transport.AddressMismatch, and the check its Subscribe contract
// asks for): that refuses the message whose routing belongs to another app, or
// names no app at all. The subscription's durable carries the app, which is what
// makes the second check expressible at all: the tenant named inside the document
// is compared with tenants.app (holdsTenant), because an address can say that a
// publisher *claims* the delivery is for this app and that tenant, and nothing
// about a tenant id says which app holds it. Both come before the third: the
// transaction, which opens *as* that tenant, so past this point row-level security
// is the tenant's own and would show this app's handler code another app's rows.
// A message stored on one tenant's address while stamped as another's is not this
// kernel's event at all, and neither is one that names a tenant this app does not
// hold; a refusal runs no handler and writes no claim, so the event stays
// replayable for the app that does hold its tenant. See claim.
//
// A subscription whose App is set but is not a slug is refused here, at boot:
// an app segment that a subject token cannot hold forms nothing, so its durable
// would be the unscoped one — some other app's consumer — and no delivery of its
// own could ever be shown to belong to it.
func Consume(ctx context.Context, conn *db.Conn, t Transport, subs []Subscription) error {
	for _, s := range subs {
		if s.Handler == nil {
			return fmt.Errorf("events: subscription %s to %s has no handler", s.Module, s.Name)
		}
		if s.App.Named() {
			// The type is not the check: a Name built by conversion bypasses Parse,
			// and a slug a subject token cannot hold forms an unscoped durable —
			// somebody else's consumer.
			if _, err := appname.Parse(string(s.App)); err != nil {
				return fmt.Errorf("events: subscription %s to %s names app %q: %w", s.Module, s.Name, string(s.App), err)
			}
		}
		h, durable := s.Handler, s.durable()
		sink := Sink{
			Handle: func(ctx context.Context, ev Event) error {
				// A handler holds a transaction, so it is bounded. The
				// acknowledgement deadline is the ladder's first rung, which
				// is shorter than this, so a handler slower than that rung is
				// redelivered while its first attempt is still writing: the
				// claim's row lock serializes the two, and the redelivery acks
				// as soon as the first attempt commits. handlerTimeout is what
				// bounds that wait. See claim, and providers/nats/jetstream.go's ackWait.
				ctx, cancel := context.WithTimeout(ctx, handlerTimeout)
				defer cancel()
				holds, err := holdsTenant(ctx, conn, s.App, ev.TenantID)
				if err != nil {
					// Nothing is known about the delivery, so nothing runs and the
					// transport retries: the outbox row stays pending and the event
					// is not lost to a database that is merely unreadable.
					return err
				}
				if !holds {
					// The same shape as a message at an address its document does
					// not claim: this copy is undeliverable, the outbox still holds
					// the row for the app that does hold the tenant, and the copy
					// that app's own relay publishes is a different message. A claim
					// row here would mark another app's event handled under this
					// durable, which is a refusal that takes the last copy away.
					slog.ErrorContext(ctx, "events: delivery names a tenant this app does not hold",
						"app", s.App.String(), "durable", durable, "event", ev.Name,
						"id", ev.ID, "tenant", ev.TenantID)
					return nil
				}
				// Only the id is known here. It is all kit/db needs to scope
				// the transaction, and it is what row-level security reads.
				ctx = tenancy.WithTenant(ctx, tenancy.Tenant{ID: ev.TenantID})
				// One span per delivery, on the trace of the work that published the
				// event rather than of the worker that woke up. See trace.go.
				ctx, span := startDelivery(ctx, ev)
				err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					// The same question again, of the transaction that is about to mark
					// the work done — and only of a subscription naming no app, because
					// that is the only subscription whose answer moves. A tenant does not
					// leave an app (migrations/000043, and 000045 writes an app onto a row
					// that is still empty), so a scoped delivery that passed holdsTenant
					// cannot be told otherwise; an app-less one can, because the placement
					// is exactly the act that turns its answer into somebody else's. See
					// holdsUnscoped.
					if !s.App.Named() {
						holds, err := holdsUnscoped(ctx, tx, ev.TenantID)
						if err != nil {
							return err
						}
						if !holds {
							// The reading outside the transaction said this tenant was
							// nobody's, and the row now says otherwise: the placement landed
							// between the two, and the app the row names answers for the
							// delivery from here. Write nothing, and refuse the copy the way
							// the check above does — a claim under the unscoped durable would
							// be a mark in the one ledger that app will never read, which is
							// the window the move exists to close, marked instead of closed.
							slog.ErrorContext(ctx, "events: delivery's tenant took an app while it was being read",
								"durable", durable, "event", ev.Name,
								"id", ev.ID, "tenant", ev.TenantID)
							return nil
						}
					}
					first, err := claim(tx, ev.ID, durable)
					if err != nil || !first {
						return err
					}
					return h(ctx, tx, ev)
				})
				endSpan(span, err)
				return err
			},
			Dead: func(ctx context.Context, ev Event, cause error) error {
				return deadLetter(ctx, conn, ev, durable, cause)
			},
		}
		if err := t.Subscribe(ctx, durable, s.Name, sink); err != nil {
			return fmt.Errorf("events: subscribe %s to %s: %w", s.Module, s.Name, err)
		}
	}
	return nil
}

// holdsTenant reports whether the tenant an event names is one `app` holds, read
// from tenants.app — the same column, compared the same way, as the relay's claim
// on the rows of its own app (RelayApp), because the two halves of one rule
// written twice drift, and drift here is one app's handler running inside another
// app's rows.
//
// Why this is a read and not something cheaper: nothing the message carries says
// which app a tenant belongs to. The address says what its publisher claimed, the
// envelope repeats the claim (it carries no app of its own), and the tenant id is
// the same vocabulary on both sides of the boundary. tenants.app is the fact, the
// relay already reads it to decide what to publish, and this is the same question
// asked of the same column at the moment it decides what to run. It costs one
// indexed read of one row per delivery, and the alternative is a delivery that
// trusts the document.
//
// A tenant row that is gone names no app, so coalesce answers `”` and the
// deployment that names no app keeps it: LEFT JOIN and coalesce are RelayApp's
// shape for the same reason, and an app that names itself reads nothing of an app
// nothing declared.
func holdsTenant(ctx context.Context, conn *db.Conn, app appname.Name, tenantID uuid.UUID) (bool, error) {
	var holds bool
	err := db.RunSystem(ctx, conn, tenantToken, func(_ context.Context, tx db.Tx[db.System]) error {
		var whose string
		// max() so the read answers '' for a tenant with no row rather than no
		// rows: an aggregate is one row whatever the WHERE did, and a delivery
		// whose tenant is gone has an answer (nobody's) rather than an error.
		row := tx.DB().Raw(`SELECT coalesce(max(tn.app), '') FROM tenants tn WHERE tn.id = ?`, tenantID).Row()
		if err := row.Scan(&whose); err != nil {
			return fmt.Errorf("events: say which app holds tenant %s: %w", tenantID, err)
		}
		holds = whose == app.String()
		return nil
	})
	return holds, err
}

// holdsUnscoped declares the tenant this delivery is written for, and only then
// re-reads — inside the delivery's own transaction — the question holdsTenant asked
// outside it: is this tenant still nobody's?
//
// It exists because the outer read is a decision made from a snapshot the write does
// not hold. A delivery reads the row, the placement names it, and the claim that
// follows marks work done under a durable the tenant's new app will never subscribe
// under — the move that renames those rows has already looked, found nothing, and
// reported its zero.
//
// The re-read alone narrowed that window to two statements and no further, because the
// reading it replaced was stale in the same way: a second nonlocking snapshot can be
// overtaken by the same placement, and the trigger's key arrives too late to help, since
// the INSERT that fires it can itself wait — behind a relation lock, or behind another
// writer's row — with nothing said about its tenant. So the lock comes first and the read
// second, and the order is the cure: once this statement has run, no move of this tenant
// can commit while the delivery is open, so the answer below is either "still nobody's,
// and nobody can become somebody before the claim commits" or "someone's, and the claim
// is not ours to write". A lock taken after a decision records it; taken before, it makes
// it authoritative.
//
// Holding it to commit costs the app-less deployment the window the claim already held
// the same key for, and costs a deployment that has moved nothing: the key is asked for
// by an app-less subscription alone, and a placement moves a tenant out of that branch.
func holdsUnscoped(ctx context.Context, tx db.Tx[db.Tenant], tenantID uuid.UUID) (bool, error) {
	// The declaration, before the reading: see above, and declareTenant for why this is
	// the half the row trigger cannot reach.
	if err := declareTenant(tx.DB(), tenantID); err != nil {
		return false, err
	}
	var whose string
	// max() so a tenant with no row answers '' rather than no rows, as holdsTenant
	// does: the row-level policy shows a tenant transaction its own row and nothing
	// else, and an app that names itself is the deployment that keeps a tenant the
	// control plane has not answered for.
	row := tx.DB().Raw(`SELECT coalesce(max(tn.app), '') FROM tenants tn WHERE tn.id = ?`, tenantID).Row()
	if err := row.Scan(&whose); err != nil {
		return false, fmt.Errorf("events: say which app holds tenant %s: %w", tenantID, err)
	}
	return whose == "", nil
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
	// Older releases wrote dead letters without a claim. They are terminal too.
	res := tx.DB().Exec("INSERT INTO "+handled+" (event_id, durable, tenant_id) SELECT ?, ?, ?"+
		" WHERE NOT EXISTS (SELECT 1 FROM "+deadLetters+" WHERE event_id = ? AND durable = ?) ON CONFLICT DO NOTHING",
		id, durable, db.TenantOf(tx).ID, id, durable)
	if res.Error != nil {
		return false, fmt.Errorf("events: claim %s for %s: %w", id, durable, res.Error)
	}
	return res.RowsAffected == 1, nil
}

// handlerTimeout bounds each transaction and any wait on another delivery's
// claim. Broker redeliveries may overlap it; the claim serializes their effects.
const handlerTimeout = 25 * time.Second

// deadLetter atomically claims a terminal outcome and records its cause. The
// same unique claim serializes this with successful or concurrent handling.
// A failed insert rolls back the claim; a lost acknowledgment of successful
// work cannot create a false dead letter. An explicit operator replay must
// remove the terminal claim as well as review the failure; relaying alone
// deliberately cannot repeat a consequential action.
func deadLetter(ctx context.Context, conn *db.Conn, ev Event, durable string, cause error) error {
	ctx, cancel := context.WithTimeout(ctx, handlerTimeout)
	defer cancel()
	return db.RunSystem(ctx, conn, deadLetterToken, func(_ context.Context, tx db.Tx[db.System]) error {
		// The same declaration a delivery makes before it reads, for the same reason and
		// with the same limit: this is a claim too, written at an unscoped durable, and a
		// terminal record that lands after a move renamed its tenant's ledger is one no
		// scoped durable will ever carry — so `claim` would let a handler run again for
		// work the kernel had already given up on. A durable that names an app is renamed
		// by nobody, so it declares nothing: the same WHEN clause as the trigger, and the
		// same key it would take once the INSERT reached its row.
		if unscopedDurable(durable) {
			if err := declareTenant(tx.DB(), ev.TenantID); err != nil {
				return err
			}
		}
		return tx.DB().Exec(`WITH claimed AS (
   INSERT INTO `+handled+` (event_id, durable, tenant_id) VALUES (?, ?, ?)
   ON CONFLICT DO NOTHING RETURNING event_id
  ) INSERT INTO `+deadLetters+` (event_id, durable, tenant_id, name, error)
   SELECT event_id, ?, ?, ?, ? FROM claimed ON CONFLICT DO NOTHING`,
			ev.ID, durable, ev.TenantID, durable, ev.TenantID, ev.Name, cause.Error()).Error
	})
}
