// Package events publishes through a transactional outbox and delivers at least
// once. Consume claims each event/subscription pair in the handler's transaction,
// so committed work is skipped on redelivery and failed work remains retryable.
// Periodic scheduling belongs to kit/jobs; see README.md for delivery limits.
//
// The [Publish] example declares an event and writes it in a tenant transaction.
// It then relays the committed event to a subscription through the in-process transport.
package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	// belongs to the tenant whose data changed, by construction. The manifest that
	// measures it is the one the composition carried on this context, and the one it
	// installed when the context carries none — see publisher.go.
	pub, _ := PublisherFrom(ctx)
	return write(ctx, tx.DB(), db.TenantOf(tx).ID, name, payload, false, pub)
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
	pub, _ := PublisherFrom(ctx)
	return write(ctx, tx.DB(), tenantID, name, payload, true, pub)
}

// write is the one INSERT. The actor is whatever kit/tenancy has on the
// context and NULL otherwise, passed as an untyped nil so the column is null
// rather than the nil UUID: "nobody" and "the user 00000000-…" are different
// answers and only one of them is true.
func write(ctx context.Context, gdb *gorm.DB, tenantID uuid.UUID, name string, payload any, system bool, pub Publisher) error {
	if !ValidName(name) {
		return fmt.Errorf("events: %q is not %q", name, "<module>.<event>")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("events: %s: marshal the payload: %w", name, err)
	}
	if err := measureManifest(gdb, pub, tenantID, name, body); err != nil {
		return err
	}
	// An attribution says the cause was not a session. The person on the context
	// then belongs in `initiator`, not `actor`: a seed run names whom it served,
	// and nobody signed in as them. With no attribution the actor is the signed-in
	// person, exactly as it was before this column existed.
	attr, hasAttr := AttributionFrom(ctx)
	if hasAttr && !attr.Valid() {
		return fmt.Errorf("events: %s: attribution %q is not a kind, file and line this kernel can store", name, attr.ActorKind)
	}
	var actor any
	if id, ok := tenancy.ActorFrom(ctx); ok && (!hasAttr || attr.ActorKind == ActorUser) {
		actor = id
	}
	kind, file, line, initiator := any(nil), any(nil), any(nil), any(nil)
	if hasAttr {
		kind, file, line, initiator = attr.ActorKind, nilIfEmpty(attr.SourceFile), nilIfZero(attr.SourceLine), nilUUID(attr.InitiatorID)
	}
	// Persist causality while the request is available so later relay and delivery
	// can reconstruct its trace, actor and origin. Missing trace members stay NULL;
	// baggage carries trace correlation while request_id supports audit queries.
	op, err := operationSnapshot(ctx, gdb, tenantID, system)
	if err != nil {
		return err
	}
	facts := operationColumns(op)
	parent, state, correlation := carriedContext(ctx)
	req, _ := request.From(ctx)
	columns := "id, tenant_id, name, payload, actor, traceparent, tracestate, baggage, request_id, client_ip, actor_kind, source_file, source_line, initiator"
	values := "?, ?, ?, ?::jsonb, ?, ?, ?, ?, ?, NULLIF(?, '')::inet, ?, ?, ?, ?"
	args := []any{uuid.New(), tenantID, name, string(body), actor, nilIfEmpty(parent), nilIfEmpty(state), nilIfEmpty(correlation), nilIfEmpty(req.ID), req.ClientAddr, kind, file, line, initiator}
	if op.ID != "" {
		columns += ", operation_id, operation_ref, operation_tenant_id, operation_person_id, operation_outcome, operation_revision"
		values += ", ?, ?, ?, ?, ?, ?"
		args = append(args, facts...)
	}
	if err := gdb.Exec("INSERT INTO "+table+" ("+columns+") VALUES ("+values+")", args...).Error; err != nil {
		return fmt.Errorf("events: %s: %w", name, err)
	}
	return nil
}

// nilIfZero stores a count that did not happen as NULL rather than 0, for the
// same reason nilIfEmpty stores an absent string as NULL: two words for one fact
// is one word too many, and the column's CHECK refuses a source line of 0.
func nilIfZero(n int) any {
	if n == 0 {
		return nil
	}
	return n
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
	// Terminal records owner-confirmed failure together with the dead letter.
	// Nil preserves causality without completing the originating operation.
	Terminal func(context.Context, db.Tx[db.Tenant], Event, error) error
}

// durable is the subscription's name on the transport. Dots separate a subject,
// so they cannot appear in a consumer name: the two halves join with a dash.
func (s Subscription) durable() string {
	return appname.Durable(s.App, s.Module, s.Name)
}

// holdsTenant checks tenants.app using the relay's ownership predicate; an
// address or envelope alone cannot establish tenant ownership. Missing tenant
// rows belong only to the unnamed deployment, matching RelayApp's LEFT JOIN.
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

// holdsUnscoped takes the tenant's shared ledger lock before re-reading placement
// inside the delivery transaction. This prevents placement from overtaking the
// ownership decision while the claim waits to insert; a named tenant is refused.
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

// claim inserts the event/subscription mark in the handler's transaction and
// returns whether this delivery owns it. A conflict waits for the prior writer, then
// reads what that mark proves: a claim that owes no deferred effect is finished work
// this delivery skips and acknowledges, and one that still owes one has not finished,
// so the copy is refused and left to the transport's ladder. Rollback leaves the work
// retryable. Scoped subscriptions also consult older unscoped marks and terminal
// records. Separate durable spellings do not serialize uncommitted claims by
// themselves; see README.md for placement limits.
func claim(tx db.Tx[db.Tenant], id uuid.UUID, durable, older string) (bool, error) {
	// One name for an app-less subscription, two for one that names an app: the
	// statement asks the same question of both ledgers and cannot ask it of a name
	// that is the same string twice.
	names := []any{durable}
	if older != "" && older != durable {
		names = append(names, older)
	}
	first, err := insertClaim(tx, id, names)
	if err != nil || first {
		return first, err
	}
	return owedOrHandled(tx, id, names)
}

// insertClaim writes the mark and reports whether this transaction wrote it. The
// two guards are the two ways the work is already spoken for: a claim under either
// spelling of this subscription's name, and — for releases that wrote a dead letter
// with no claim beside it — a terminal record, which is as finished as a claim.
func insertClaim(tx db.Tx[db.Tenant], id uuid.UUID, names []any) (bool, error) {
	n := len(names)
	q := "INSERT INTO " + handled + " (event_id, durable, tenant_id) SELECT ?, ?, ?" +
		" WHERE NOT EXISTS (SELECT 1 FROM " + deadLetters + " WHERE event_id = ? AND " + durableSet(n) + ")" +
		" AND NOT EXISTS (SELECT 1 FROM " + handled + " WHERE event_id = ? AND " + durableSet(n) + ")"
	args := append([]any{id, names[0], db.TenantOf(tx).ID, id}, names...)
	args = append(args, id)
	args = append(args, names...)
	res := tx.DB().Exec(q+" ON CONFLICT DO NOTHING", args...)
	if res.Error != nil {
		return false, fmt.Errorf("events: claim %s for %v: %w", id, names, res.Error)
	}
	return res.RowsAffected == 1, nil
}

// owedOrHandled reads the claim this delivery conflicted with, under its lock, and
// says which of two facts the conflict proves: the work is finished, or somebody
// still owes effects it deferred past its own commit.
//
// The lock is the point of the read: the attempt that wrote the row may still be
// running its effects, and the value that decides this answer is one it is about to
// change. FOR UPDATE takes the row from them — either the last count is in, or this
// delivery waits for the attempt that owes and reads what it settled on.
//
// The row can also be gone: its attempt released the claim after this transaction's
// zero-row INSERT, or the purge took it. A terminal record beside it is still an
// answer; with neither record the work is for the taking, so the INSERT is retried
// once and a second conflict is refused rather than guessed at.
func owedOrHandled(tx db.Tx[db.Tenant], id uuid.UUID, names []any) (bool, error) {
	var owed int
	err := tx.DB().Raw("SELECT effects_owed FROM "+handled+" WHERE event_id = ? AND "+durableSet(len(names))+
		" FOR UPDATE", append([]any{id}, names...)...).Row().Scan(&owed)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, gorm.ErrRecordNotFound) {
		// No claim to read the mark off. A terminal record is the other way this
		// event is finished — deadLetter writes its claim beside the letter, and older
		// releases wrote the letter alone — and neither record means the work is there
		// for the taking.
		terminal, terr := terminalRecorded(tx, id, names)
		if terr != nil {
			return false, terr
		}
		if terminal {
			return false, nil
		}
		return claimAfterTheRowWentAway(tx, id, names)
	}
	if err != nil {
		return false, fmt.Errorf("events: ask whether %s still owes an effect: %w", id, err)
	}
	if owed > 0 {
		return false, fmt.Errorf("events: %s is claimed by a delivery whose effect has not run: %w", id, ErrDeliveryOwesEffect)
	}
	return false, nil
}

// terminalRecorded reports whether this event already has a dead letter under
// either spelling of this subscription's name.
func terminalRecorded(tx db.Tx[db.Tenant], id uuid.UUID, names []any) (bool, error) {
	var recorded bool
	err := tx.DB().Raw("SELECT EXISTS (SELECT 1 FROM "+deadLetters+" WHERE event_id = ? AND "+durableSet(len(names))+")",
		append([]any{id}, names...)...).Row().Scan(&recorded)
	if err != nil {
		return false, fmt.Errorf("events: ask whether %s is dead-lettered: %w", id, err)
	}
	return recorded, nil
}

// claimAfterTheRowWentAway takes the claim a conflict no longer stands on, once
// terminalRecorded has said nothing ended this event. One row affected means this
// delivery is the first to do the work after all; none means a claim this delivery
// has never seen appeared while it was reading, which it must not run over.
func claimAfterTheRowWentAway(tx db.Tx[db.Tenant], id uuid.UUID, names []any) (bool, error) {
	first, err := insertClaim(tx, id, names)
	if err != nil || first {
		return first, err
	}
	return false, fmt.Errorf("events: %s is claimed again as fast as it is read: %w", id, ErrDeliveryOwesEffect)
}

// handlerTimeout bounds each transaction and any wait on another delivery's
// claim. Broker redeliveries may overlap it; the claim serializes their effects.
const handlerTimeout = 25 * time.Second

// deadLetter claims a terminal outcome and records its cause atomically, using
// the same unique claim as handling. It locks the tenant before reading placement
// and writes under the current app's durable, so exhausted work remains findable, and
// a claim that still owes a deferred effect is terminalised beside its letter rather
// than left unrecorded. Operator replay must clear the terminal claim as well as the
// failure record.
func deadLetter(ctx context.Context, conn *db.Conn, ev Event, durable string, cause error) error {
	ctx, cancel := context.WithTimeout(ctx, handlerTimeout)
	defer cancel()
	return db.RunSystem(ctx, conn, deadLetterToken, func(_ context.Context, tx db.Tx[db.System]) error {
		// The same declaration a delivery makes before it reads, and for the same reason:
		// this is a claim too, written at an unscoped durable, so the question below is
		// asked under the key that makes its answer one a move has to respect. A durable
		// that names an app is renamed by nobody, so it declares nothing and asks nothing:
		// the same WHEN clause as the trigger, and the same key it would take once the
		// INSERT reached its row.
		recorded := durable
		if unscopedDurable(durable) {
			if err := declareTenant(tx.DB(), ev.TenantID); err != nil {
				return err
			}
			named, err := terminalDurable(ctx, ev, durable, tx.DB())
			if err != nil {
				return err
			}
			recorded = named
		}
		return tx.DB().Exec(`WITH claimed AS (
   INSERT INTO `+handled+` (event_id, durable, tenant_id) VALUES (?, ?, ?)
   ON CONFLICT DO NOTHING RETURNING event_id
  ), owed AS (
   SELECT event_id FROM `+handled+` WHERE event_id = ? AND durable = ? AND effects_owed > 0
  ), ending AS (
   SELECT event_id FROM claimed UNION SELECT event_id FROM owed
  ) INSERT INTO `+deadLetters+` (event_id, durable, tenant_id, name, error)
   SELECT event_id, ?, ?, ?, ? FROM ending ON CONFLICT DO NOTHING`,
			ev.ID, recorded, ev.TenantID, ev.ID, recorded,
			recorded, ev.TenantID, ev.Name, cause.Error()).Error
	})
}

// terminalDurable answers which durable this terminal outcome belongs to, of the
// tenant it was recorded for and the durable the delivery arrived on.
//
// A tenant nobody placed keeps the delivered name: the app-less deployment is the
// one that reads it (ledger.go's predicate for what a move may not touch), and
// prefixing a row for an app nobody declared would be the write that takes its
// claim away from the consumer that has it.
//
// A tenant whose app is set but is not a slug names a durable no consumer could
// ever be created under — the refusal Consume and MoveLedger both make at boot —
// so it keeps the delivered name too, and says so in the log: a mark under an
// unnameable durable would be a dead letter no subscription and no replay could
// ever find, which is the row this function exists to place.
func terminalDurable(ctx context.Context, ev Event, delivered string, gdb *gorm.DB) (string, error) {
	whose, err := appOfTenant(gdb, ev.TenantID)
	if err != nil {
		return "", err
	}
	if !appname.Name(whose).Named() {
		return delivered, nil
	}
	app, err := appname.Parse(whose)
	if err != nil {
		slog.ErrorContext(ctx, "events: a terminal failure's tenant names an app no durable can be formed from",
			"tenant", ev.TenantID, "event", ev.Name, "id", ev.ID, "durable", delivered, "error", err)
		return delivered, nil
	}
	return appname.DurablePrefix(app) + delivered, nil
}
