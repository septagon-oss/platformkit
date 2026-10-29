package events

// deferred.go is the other half of what a delivery may acknowledge.
//
// Consume claims a delivery inside the handler's own transaction, so the claim and
// the handler's rows commit together. An effect that waits for that commit breaks
// the identity: the claim is in by the time the effect runs, so the effect can
// neither roll the claim back nor be remembered by it. Without the mark below the
// loss is twice over — the effect fails, the transport retries, the retry finds the
// claim taken, skips the handler and acknowledges work nobody finished.
//
// So a claim knows one more fact: how many effects its delivery still owes, counted
// because a handler may defer several and kit/db runs them as one queue. The column
// is written in the delivery's own transaction so it appears exactly when the claim
// does, and read under that row's lock by the next delivery. A delivery that owes
// nothing acknowledges; one that owes leaves the message to the transport's ladder.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
)

// ErrDeliveryOwesEffect says this delivery's mark is held by an attempt whose
// deferred effects have not all carried out. It is not a refusal of the work and not
// a record that the work was done: nothing acknowledges it, the transport retries on
// its own ladder, and the attempt that owes an effect releases the claim if that
// effect finally fails.
var ErrDeliveryOwesEffect = errors.New("events: this delivery still owes an effect that has not run")

// Defer registers action as an effect this work owes, to run once the tenant
// transaction now open on ctx has committed and never after a rollback — see
// db.AfterCommit for the hand-off itself.
//
// Outside a delivery that is all it does: a request that defers an effect is
// answered by its own caller, which learns the failure from Run, and nothing marks
// the effect as owed because no transport is retrying the call.
//
// Inside a delivery it additionally adds one to the count of effects this claim owes,
// in that transaction, and wraps the action so that success takes one off the count in
// a transaction of its own and failure leaves it standing. The count is what stops a
// redelivery mid-flight acknowledging an effect nobody carried out: it reaches zero
// only once every effect this delivery deferred has come back. Refuses with
// db.ErrNoTransactionToDefer when ctx carries no tenant transaction.
func Defer(ctx context.Context, action func(context.Context) error) error {
	if action == nil {
		return fmt.Errorf("%w: a nil action would be dropped without ever being noticed", db.ErrNoTransactionToDefer)
	}
	carried, inDelivery := deliveryOf(ctx)
	if !inDelivery {
		return db.AfterCommit(ctx, action)
	}
	tx, ok := db.TxFrom(ctx)
	if !ok {
		return db.ErrNoTransactionToDefer
	}
	if err := oweEffect(tx, carried.event.ID, carried.durable); err != nil {
		return err
	}
	return db.AfterCommit(ctx, func(ctx context.Context) error {
		if err := action(ctx); err != nil {
			// The count stays where it was, so a redelivery cannot acknowledge what
			// this attempt failed to do; Consume releases the claim beside this error,
			// so the handler itself runs again rather than being skipped.
			return err
		}
		return markEffectRun(ctx, carried.event.ID, carried.durable)
	})
}

// deliveryOf is the delivery this context is handling, if any.
func deliveryOf(ctx context.Context) (commandDelivery, bool) {
	d, ok := ctx.Value(commandDeliveryKey{}).(commandDelivery)
	return d, ok
}

// DeliveryOf reports the id of the event whose delivery ctx is the handling of, and
// false for work that is no delivery: a request, a periodic job, a boot.
//
// A handler is handed its event; this is for the code underneath one, which takes
// no event and is given no argument for it — a module that has to say which delivery
// wrote a row, or decide whether it is answering a transport or a person, asks here.
func DeliveryOf(ctx context.Context) (uuid.UUID, bool) {
	d, ok := deliveryOf(ctx)
	if !ok {
		return uuid.Nil, false
	}
	return d.event.ID, true
}

// oweEffect adds one effect to what this delivery's claim owes, on the row this
// delivery's own transaction inserted. The UPDATE is the lock as well as the write,
// and the row it must find is the one this transaction just inserted: a mark that
// landed on somebody else's claim would owe an effect to the wrong delivery. A
// handler that defers twice asks twice, because each effect is owed on its own.
func oweEffect(tx db.Tx[db.Tenant], id uuid.UUID, durable string) error {
	res := tx.DB().Exec("UPDATE "+handled+" SET effects_owed = effects_owed + 1 WHERE event_id = ? AND durable = ?", id, durable)
	if res.Error != nil {
		return fmt.Errorf("events: mark %s owed an effect: %w", id, res.Error)
	}
	if res.RowsAffected != 1 {
		return fmt.Errorf("events: %s is claimed under no mark this transaction wrote (%s)", id, durable)
	}
	return nil
}

// markEffectRun takes one effect off the count once that effect succeeded, in a
// transaction of its own: the one it was deferred out of has committed, and what is
// being said is that this effect of that commit really happened. The arithmetic is
// the row's own and not a value read first, because the delivery's other effects are
// outstanding in the same row and the count is theirs to keep: a claim whose handler
// deferred two effects still owes one after the first comes back.
//
// Its failure is returned rather than logged, and the consequence is deliberate: a
// claim left counting itself owed makes every redelivery refuse, which is safer than
// the opposite — a count brought to zero early is an ack over an effect nobody
// confirmed. The attempt that fails here releases its claim, and the handler runs
// again to find its work already done and finish the delivery without sending
// anything twice.
func markEffectRun(ctx context.Context, id uuid.UUID, durable string) error {
	conn, ok := db.ConnFrom(ctx)
	if !ok {
		return fmt.Errorf("events: the effect of %s ran on a context carrying no connection, so its mark cannot be cleared", id)
	}
	err := db.Run(db.Detached(ctx), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("UPDATE "+handled+" SET effects_owed = effects_owed - 1 WHERE event_id = ? AND durable = ?", id, durable).Error
	})
	if err != nil {
		return fmt.Errorf("events: the effect of %s ran and its claim still says otherwise: %w", id, err)
	}
	return nil
}

// releaseToken is the capability the release below needs. A system transaction
// because the tenant transaction that wrote the claim is closed by the time its
// deferred effects fail, and the failure may be that a new one cannot be opened.
var releaseToken = syscap.NewSystemToken("release the claim of a delivery whose deferred effect did not run")

// releaseClaim takes back one subscription's mark against one event, so the next
// delivery of it is a first delivery and not a skip.
//
// It deletes one row and names it twice — the event and the durable, which is the
// whole primary key — because another subscription's mark on the same event is
// another piece of work. What it costs is stated in claim: the redelivery that
// arrives after the release runs the handler rather than waiting on its lock, so a
// handler whose effect failed can have its rows written twice. That is at-least-once,
// which is what the transport contract always was; the claim is what usually
// improves on it, and a delivery still owed an effect is where it must not stand.
func releaseClaim(ctx context.Context, conn *db.Conn, id uuid.UUID, durable string) error {
	// WithoutCancel: the deadline that timed out this attempt must not also veto the
	// record of it. The transport is about to retry, and a claim left held by an
	// attempt that ran out of time is work nobody can see.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), handlerTimeout)
	defer cancel()
	return db.RunSystem(ctx, conn, releaseToken, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec("DELETE FROM "+handled+" WHERE event_id = ? AND durable = ?", id, durable).Error
	})
}
