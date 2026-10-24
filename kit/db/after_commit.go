package db

// after_commit.go defers an effect out of a transaction and into its commit.
//
// It exists for one shape of work and no other: an effect that must not be
// observable while the rows that make it valid are still uncommitted, and that
// cannot itself be a row. The outbox covers the effect that can be a row; what it
// cannot carry is a secret, because a payload is kept for a week and modules/audit
// copies every one into the trail.
//
// Handing such an effect over inside the transaction loses both ways: a rollback
// leaves the effect out there with nothing behind it, and a reader who has it can
// reach work a moment before the transaction authorising it is visible — a refusal
// that caller can neither explain nor retry.

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrNoTransactionToDefer is returned by AfterCommit when the context carries no
// tenant transaction that could run the action: none at all, or a system one. A
// system transaction is refused rather than supported because only kit/ mints the
// token that opens one, so an effect deferred from it would be a kernel effect on
// one tenant's data, decided by code that has no business deciding it.
var ErrNoTransactionToDefer = errors.New("db: no tenant transaction to wait for")

// ErrEffectNotRun says the transaction committed and one of the effects deferred
// to after its commit did not happen. Nothing else distinguishes "committed,
// effect missing" from a rollback a moment later, and the two need different
// answers: a rollback is retried by the caller, and a committed row is not.
var ErrEffectNotRun = errors.New("db: the transaction committed and a deferred effect did not run")

// afterAction is one deferred effect and the context it was registered with.
//
// The context travels with the action because the commit has no context of its
// own: Close is called by Run, by the request middleware and by the panic path,
// and what the action should be traced under is the work that asked for it. That
// context is still live — a transaction ends before its request does — and when
// it is not, the action sees a cancelled context and says so.
type afterAction struct {
	ctx context.Context
	fn  func(context.Context) error
}

// commitQueue is the actions one transaction will run after it commits. It is
// reached through the Pending that will end the work, so an action registered by a
// nested Run joins the commit rather than the frame that registered it.
type commitQueue struct {
	mu      sync.Mutex
	actions []afterAction
}

func (q *commitQueue) add(a afterAction) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.actions = append(q.actions, a)
}

// drain takes the whole queue, so a transaction that is closed twice — Close is
// idempotent — runs its actions once. A rollback drops them rather than deferring
// them to a commit that will not come.
func (q *commitQueue) drain() []afterAction {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	a := q.actions
	q.actions = nil
	return a
}

// AfterCommit registers action to run once the tenant transaction now open on ctx
// has committed, before Run (or the request holding the transaction) returns. It
// runs on a successful commit and on nothing else: a handler error, a failed commit,
// a refused settings re-read, a rolled-back request or a panic runs none of the
// queue. An effect that must survive a rollback does not belong here.
//
// The action is handed a context and no handle: whatever it needs from the database
// it reads inside the transaction before registering, and a query it must make
// afterwards opens its own with db.Run on db.Detached(ctx), on the pool that
// transaction ran on — which db.ConnFrom carries.
//
// An action's error cannot undo the commit, which is nothing a function can do
// after the fact. It comes back from Close wrapping ErrEffectNotRun; every action
// runs even after one before it failed, being registered by different work.
func AfterCommit(ctx context.Context, action func(context.Context) error) error {
	if action == nil {
		return fmt.Errorf("%w: a nil action would be dropped without ever being noticed", ErrNoTransactionToDefer)
	}
	cur, ok := current(ctx)
	if !ok || cur.system || cur.hooks == nil {
		return ErrNoTransactionToDefer
	}
	cur.hooks.add(afterAction{ctx: ctx, fn: action})
	return nil
}

// runAfter applies a drained queue in registration order and reports every
// failure joined. Each action runs on a detached context carrying conn: the
// transaction is over, and an action that has to write is writing after it, so it
// needs the pool that transaction ran on and has no other way to name it. The pool
// travels on the context rather than as an argument because an action's signature is
// the one AfterCommit publishes, and a queue drained by a Pending always knows which
// pool it belongs to — it is the one that just committed.
func runAfter(conn *Conn, actions []afterAction) error {
	var errs []error
	for _, a := range actions {
		ctx := Detached(a.ctx)
		if conn != nil {
			ctx = WithConn(ctx, conn)
		}
		if err := a.fn(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %d of its %d deferred actions failed: %w",
		ErrEffectNotRun, len(errs), len(actions), errors.Join(errs...))
}
