package events

// replay.go is the operator's verb: an event whose delivery ended in a dead
// letter is given another run.
//
// The relay cannot do this, and nothing else can. A terminal failure writes a
// claim and a dead letter in one statement (deadLetter), and every later
// delivery finds the claim taken and skips the handler — that is the guarantee
// that makes at-least-once delivery safe to run at all. So re-running an action
// means removing the claim, and removing a claim is removing the record that a
// consequential action already happened. That is an operator's decision, made
// under a name, with a reason, and it is the one write in this package that is
// not a machine repeating itself. Hence: a capability of its own
// (docs/adr/0006), the actor required of the request's context, a reason that has
// to say itself, and the act recorded as an event in its own right.
//
// What is re-stamped rather than rewritten: published_at goes back to NULL, so
// the relay carries the same row — the same id, the same payload, the same
// tenant and the same trace — again under the same at-least-once rules. Nothing
// is duplicated, because nothing new was published.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// EventReplayed names the record that an operator replayed an event. It is the
// kernel's own event, and kit/app declares it in the composition's module list,
// so the audit module — which subscribes to every event the composition emits —
// records the act in the tenant's own trail, with the operator as its actor.
const EventReplayed = "platformkit.event_replayed"

// replayToken is the capability a replay needs: it crosses tenants to find one
// row, and it deletes rows whose whole purpose is to say a delivery is finished.
// It is a fourth token rather than a reuse of the relay's or the purge's because
// the reason a system transaction opens has to be its own — that is what the
// log line says, and what an audit of the capability answers.
var replayToken = syscap.NewSystemToken("operator replay of a terminal event")

// ErrNothingToReplay is the refusal for an id with no outbox row. It is not the
// write that finds none and does nothing: the row is the only place a payload
// survives, and the dead letter beside it is the last account of why the
// delivery stopped. Clearing either with no row left to relay would destroy the
// evidence and produce no delivery — the write that takes the last one away.
// So: refuse, write nothing, emit nothing, and say which refusal it was.
var ErrNothingToReplay = errors.New("events: no outbox row to replay")

// ReplayRecord is the payload of EventReplayed: who replayed what, and what
// they said why. It carries no payload and no secret — the trail already holds
// the event itself, and the outbox row still holds its body.
type ReplayRecord struct {
	// EventID is the replayed event, which is also the id every subscription
	// has already claimed once. That is deliberate: a subscriber that did the
	// work and is being asked again should say so, not do it twice.
	EventID uuid.UUID `json:"eventId"`
	// Name is the replayed event's name, carried so the trail row says what
	// happened without a second lookup.
	Name string `json:"name"`
	// Durable is the subscription whose claim was cleared, and empty when the
	// replay covers every subscription that claimed the event.
	Durable string `json:"durable,omitempty"`
	// Reason is the operator's own sentence. It is required: a replay that
	// cannot say why is not a decision anyone can review afterwards.
	Reason string `json:"reason"`
}

// replayRow is the one row the verb needs, read under a lock before anything is
// cleared. PublishedAt is not among them: the verb nulls it unconditionally.
type replayRow struct {
	Name     string
	TenantID uuid.UUID
}

// ErrEventOwnedByAnotherApp is the refusal of a replay asked of a deployment that
// does not consume the event: its tenant belongs to another app, or to none, so the
// relay that would carry it again belongs elsewhere. A sentinel rather than a bare
// sentence because the installation's door has to answer "the other installation
// is the one that can do this" as a status it can name, and because the caller who
// could correct this asked the wrong process, not the wrong question.
var ErrEventOwnedByAnotherApp = errors.New("events: the event belongs to another app's tenant")

// Replay gives one event another run and records the act.
//
// The caller names no app, so this form asks nothing about whose event it is: it is
// the verb for a process that consumes whatever reaches it, which is the state an
// app-less deployment is in. A deployment that names itself calls ReplayForApp, and
// gets the row's tenant asked before anything is cleared.
//
// durable names one subscription's claim, or empty for every subscription that
// claimed this event — the honest default, since an operator replaying a
// business action usually means all of its consequences, and each subscription
// still decides for itself whether its own work is already done.
//
// Everything is one system transaction. The outbox row is locked and checked
// first, so a relay carrying the same row and a replay of it cannot both get
// half their way; then the claims go, then the dead letters, then the stamp,
// then the record is written. A refusal writes nothing at all, and the sentinel
// names which refusal it was.
//
// An actor is required of the context, beside the reason and for the same
// reason: removing a claim is removing the record that a consequential action
// already happened, and a decision nobody is named on is not a decision anyone
// can read afterwards. ADR 0006 says the capability is not the authorization —
// the system token says this process may cross tenants, and only the actor says
// a person asked. kit/tenancy names the actor and kit/httpx puts it there, so a
// context that carries none is a call with nobody behind it. Reading the actor
// is not requiring it; this requires it, before the transaction opens, so the
// refusal writes nothing, emits nothing, and leaves every claim and dead letter
// where they were. The record it refuses to write is the answer to "actor,
// tenant, what": an actor column left NULL by a verb this consequential answers
// "nobody" while the write still happened.
//
// It is unconditional. A dead letter is reviewed late, so the row a replay
// relays is usually old, and age invites the thought that an old enough one
// needs no name. It does not: whether a person has to be named for deleting the
// record of a finished delivery cannot turn on how many days ago that delivery
// finished — that would waive the audit exactly where it is worth most. Who
// asked, and why, are the only questions this verb asks.
//
// Asked, not granted. Permission is held behind kit/httpx's per-route authorizer
// and this verb has no route, so what it can require is that a person be named,
// not that they hold a grant. Reaching for an authorizer through a package-level
// variable would be discovering a dependency rather than naming one, so the
// product mounts the surface, declares the permission, and asks the question there.
func Replay(ctx context.Context, conn *db.Conn, eventID uuid.UUID, durable, reason string) (ReplayRecord, error) {
	return replay(ctx, conn, appname.Name(""), eventID, durable, reason)
}

// ReplayForApp is Replay asked by the app that consumes the event. The name is the
// caller's own — this process, not the body's say-so — and it is checked against the
// row inside the transaction, after the row is locked and before the first claim is
// cleared, so a refusal writes nothing, emits nothing, and leaves the claims, the
// dead letters and the publication stamp exactly as they were. An id is not a
// boundary: it names a row, and the row's tenant names the app that holds it, so the
// comparison belongs where the write happens.
func ReplayForApp(ctx context.Context, conn *db.Conn, app appname.Name, eventID uuid.UUID, durable, reason string) (ReplayRecord, error) {
	return replay(ctx, conn, app, eventID, durable, reason)
}

// orNoApp words the answer an event's tenant gives about who holds it, for the
// sentence that refuses a replay to the wrong deployment. It says which of the two
// states the row is in — another app's, or nobody's yet — without naming the other
// slug: the operator who can act on this reads tenants.app themselves, and a log
// line that answers "whose is it" with another customer's slug answers nothing here.
func orNoApp(holder string) string {
	if holder == "" {
		return "no app yet"
	}
	return "another app"
}

func replay(ctx context.Context, conn *db.Conn, app appname.Name, eventID uuid.UUID, durable, reason string) (ReplayRecord, error) {
	// An empty reason is a correctable refusal: the same call with a sentence
	// attached is allowed. It is checked before the transaction opens, because
	// nothing about the database can make an unstated reason sufficient.
	if reason == "" {
		return ReplayRecord{}, fmt.Errorf("events: replay %s: a reason is required; it is what the trail will record", eventID)
	}
	// No actor, no replay. Checked beside the reason and for the reason in the
	// comment above: nothing about the database can make an unnamed caller
	// sufficient either, and checking here means the refusal never opened a
	// transaction, never locked the row and never cleared anything.
	if _, ok := tenancy.ActorFrom(ctx); !ok {
		return ReplayRecord{}, fmt.Errorf("events: replay %s: an actor is required; the record of the act names the operator who ordered it", eventID)
	}
	// No check on durable's spelling: a durable that names no subscription
	// clears nothing and restamps the row, which is what "found none" means
	// here. The grammar of a durable belongs to Subscription.durable, and
	// refusing a name this function cannot enumerate would refuse a replay that
	// has every right to happen.
	rec := ReplayRecord{EventID: eventID, Durable: durable, Reason: reason}
	err := db.RunSystem(ctx, conn, replayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		var rows []replayRow
		const q = "SELECT name, tenant_id FROM " + table + " WHERE id = ? FOR UPDATE"
		if err := tx.DB().Raw(q, eventID).Scan(&rows).Error; err != nil {
			return fmt.Errorf("events: replay %s: read the outbox: %w", eventID, err)
		}
		if len(rows) == 0 {
			return ErrNothingToReplay
		}
		rec.Name = rows[0].Name
		tenantID := rows[0].TenantID

		// Whose event this is, asked of the row's own tenant, and before the first
		// DELETE. A claim names no app, so the outbox row's tenant is the only fact
		// that says which deployment consumes it — the same fact MoveLedger moves a
		// ledger by. Without it the verb is a delete keyed on an id: an installation
		// holding the grant could clear the claims and terminal failures of an event
		// it will never deliver, and re-stamp a row its own relay does not read —
		// one app removing the record that another app's handler already finished.
		// A tenant that belongs to no app is refused by the same sentence: this
		// process is not the deployment whose consumer will carry the row either.
		if app.Named() {
			var holder string
			if err := tx.DB().Raw(`SELECT COALESCE(app, '') FROM tenants WHERE id = ?`, tenantID).Scan(&holder).Error; err != nil {
				return fmt.Errorf("events: replay %s: name the app that holds its tenant: %w", eventID, err)
			}
			if holder != app.String() {
				return fmt.Errorf("events: replay %s: its tenant belongs to %s; the deployment that consumes the event replays it; nothing was cleared: %w",
					eventID, orNoApp(holder), ErrEventOwnedByAnotherApp)
			}
		}

		// The claims first, and each statement bounds itself to what the verb
		// was asked for: one durable's claim, or every subscription's.
		claims, args := "event_id = ?", []any{eventID}
		if durable != "" {
			claims, args = "event_id = ? AND durable = ?", []any{eventID, durable}
		}
		if err := tx.DB().Exec("DELETE FROM "+handled+" WHERE "+claims, args...).Error; err != nil {
			return fmt.Errorf("events: replay %s: clear the handling claims: %w", eventID, err)
		}
		if err := tx.DB().Exec("DELETE FROM "+deadLetters+" WHERE "+claims, args...).Error; err != nil {
			return fmt.Errorf("events: replay %s: clear the terminal failures: %w", eventID, err)
		}
		// Back to pending: the relay carries this same row again on its next
		// tick. The stamp is the verb's whole effect on delivery, and it is the
		// database clock that sets the new one, as it sets every other.
		if err := tx.DB().Exec("UPDATE "+table+" SET published_at = NULL WHERE id = ?", eventID).Error; err != nil {
			return fmt.Errorf("events: replay %s: review the row: %w", eventID, err)
		}
		// And the record of the act, in the tenant whose event it was, written
		// here so it commits with the clearings or not at all. Audit records
		// it as it records every event; the actor is the operator's, read off
		// the request context by the one INSERT every event passes through.
		return PublishFor(ctx, tx, tenantID, EventReplayed, rec)
	})
	if err != nil {
		return ReplayRecord{}, err
	}
	return rec, nil
}
