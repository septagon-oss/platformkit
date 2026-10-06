package db

// after_commit.go defers an effect out of a transaction and into its commit.
//
// It exists for one shape of work and no other: an effect that must not be
// observable by anybody while the rows that make it valid are still uncommitted,
// and that cannot itself be a row. The outbox already solves the case where the
// effect can be a row — it commits with the rest and a worker carries it out
// afterwards. What the outbox cannot carry is a secret: an outbox payload is kept
// for a week and modules/audit copies every payload into the audit trail, so a
// one-time credential written into one is a live credential in a table nobody
// treats as a credential store. The link modules/auth mails is that effect, and it
// is the only caller in this repository. The alternative — hand the credential
// over inside the transaction and let the commit follow — loses both ways: a
// rollback leaves a link in an inbox that opens nothing, and the mail can be read
// before the transaction that authorised it is visible.
//
// The alternative — hand the credential over inside the transaction and let the
// commit follow — loses in both directions at once. A rollback leaves a link in
// somebody's inbox that opens nothing, and a reader who has the mail can reach
// the effect a moment before the transaction that authorises it is visible,
// which is a refusal the caller cannot explain and cannot retry.

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrNoTransactionToDefer is returned by AfterCommit when the context carries no
// transaction that could run the action: none at all, or a system one. A system
// transaction is refused and not supported because a cross-tenant transaction is a
// kernel operation — only kit/ mints the token that opens one — and an effect
// deferred from one would be a kernel effect on one tenant's data.
var ErrNoTransactionToDefer = errors.New("db: no tenant transaction to wait for")

// afterAction is one deferred effect and the context it was registered with.
//
// The context travels with the action because the commit has no context of its
// own: Pending.Close is called by Run, by the request middleware and by the panic
// path, and what the action should be traced under is the work that asked for it.
// That context is still live — the transaction ends before its request does — and
// when it is not, the action sees a cancelled context and says so.
type afterAction struct {
	ctx context.Context
	fn  func(context.Context) error
}

// commitQueue is the actions one transaction will run after it commits. It is
// reached through the Pending that will end the work, so an action registered by
// a nested Run joins the commit and not the frame that registered it.
type commitQueue struct {
	mu      sync.Mutex
	actions []afterAction
}

func (q *commitQueue) add(a afterAction) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.actions = append(q.actions, a)
}

// drain takes the whole queue. A transaction commits once, so its actions run
// once, and a rollback drops them rather than deferring them again.
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

// AfterCommit registers action to run once the tenant transaction now open on
// ctx has committed, before Run (or the request that holds the transaction)
// returns. It runs on a successful commit and on nothing else: a handler error, a
// failed commit, a rolled-back request, a panic — none runs it, and a rolled-back
// transaction leaves nothing for the action to be about. That is the whole reason
// to use it: it buys nothing for an effect that is a row, which commits anyway,
// and less than nothing for one that must survive a rollback, because it will not.
//
// The action carries no handle and may no longer query what it waited for, so the
// work it does must already be described: a caller that needs anything from the
// database — the host a link is addressed at — reads it inside the transaction and
// hands the action what it read. A query an action must make afterwards opens its
// own transaction, because the context it is run on carries none.
//
// An action's error does not undo the commit, which is nothing a function can do
// after the fact. It is returned, so the caller learns the rows are in and the
// effect is not, and a worker that retries finds the state it wrote and can tell
// that from the state it wanted.
func AfterCommit(ctx context.Context, action func(context.Context) error) error {
	if action == nil {
		return fmt.Errorf("%w: a nil action would be dropped without ever being noticed", ErrNoTransactionToDefer)
	}
	cur, ok := current(ctx)
	if !ok || cur.hooks == nil {
		return ErrNoTransactionToDefer
	}
	cur.hooks.add(afterAction{ctx: ctx, fn: action})
	return nil
}

// runAfter applies drained actions in order and reports every failure.
//
// Every action runs even after one before it failed: they were registered by
// different work inside one transaction, and one that did not happen is not a
// reason to skip the rest. The errors come back joined, so nothing is lost. Each
// action runs on a detached context: the transaction is over, and an action that
// asks to query is asking for a new one.
func runAfter(actions []afterAction) error {
	var errs []error
	for _, a := range actions {
		if err := a.fn(Detached(a.ctx)); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("db: the transaction committed and %d of its %d deferred actions did not run: %w",
		len(errs), len(actions), errors.Join(errs...))
}
