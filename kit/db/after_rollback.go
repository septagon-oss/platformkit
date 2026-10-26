package db

// after_rollback.go is the other half of after_commit.go: the effect that exists because
// the work was discarded. The outbox cannot carry it — a rolled back transaction has no
// row to relay — and AfterCommit drops it with the rows. README.md, "An effect that waits
// for the rollback", holds which ending runs which queue.

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// rollbackGrace bounds an effect no caller is waiting for: the request that rolled back
// has often hung up already, so nothing is left to time it out.
const rollbackGrace = 5 * time.Second

// AfterRollback registers action to run once the tenant transaction now open on ctx has
// been rolled back and its outcome is known — no COMMIT can reach the server from here —
// before Run (or the request holding the transaction) returns. It is AfterCommit's
// sibling, for the one effect whose reason to exist is that the work was discarded: bytes
// no committed row will ever name. A refused ROLLBACK, or a COMMIT that returned an error
// (which may have committed), leaves the outcome unknown and runs nothing; an ending under
// an owner that had already ended is known whatever that ROLLBACK answered.
//
// Refused with ErrNoTransactionToDefer when ctx carries no tenant transaction that could
// run it: none, one that never opened, one whose ending has been decided — the frame a Run
// handler keeps past Run's return names a queue that ending took — a system one, or a nil
// action. The action gets the context AfterCommit hands its own, carrying the pool it ran
// on, no longer cancelled by the owner and bounded by five seconds. Its error is logged and
// changes nothing Rollback or Run returns.
func AfterRollback(ctx context.Context, action func(context.Context) error) error {
	if action == nil {
		return fmt.Errorf("%w: a nil action would be dropped without ever being noticed", ErrNoTransactionToDefer)
	}
	cur, ok := current(ctx)
	if !ok || cur.system || cur.rollbacks == nil {
		return ErrNoTransactionToDefer
	}
	if !cur.rollbacks.add(afterAction{ctx: ctx, fn: action}) {
		return ErrNoTransactionToDefer
	}
	return nil
}

// runRollbacks applies a drained queue in registration order on a context the owner's
// cancellation cannot reach, and keeps every failure for the log: the caller already holds
// the answer that its work was refused, so an action's failure enters nothing it returns.
func runRollbacks(conn *Conn, actions []afterAction) {
	for _, a := range actions {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(Detached(a.ctx)), rollbackGrace)
		if conn != nil {
			ctx = WithConn(ctx, conn)
		}
		if err := a.fn(ctx); err != nil {
			slog.ErrorContext(ctx, "db: an effect deferred to after the rollback failed", "error", err)
		}
		cancel()
	}
}
