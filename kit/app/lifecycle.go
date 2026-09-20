// This file is the seam for an application that owns its own listener: Run is a
// whole application and holds the address it serves on; Start is the same
// sequence stopped before the port, so the ordering and the gates stay Run's and
// this adds ownership rather than a second startup path.
package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
)

// Runtime is a started application whose listener belongs to its caller. Start is
// its only constructor. It owns the connection Start opened and, for every role
// that runs a worker half, the transport Start opened beside it.
type Runtime struct {
	app     *App
	conn    *db.Conn
	handler http.Handler

	// transport is nil exactly when the role runs no worker half: role web must
	// not need a reachable broker to serve. Start builds it for the other roles
	// before returning, so an unreachable broker refuses this caller's boot the
	// same way it refuses Run's — before anything is served. Reachable and wired
	// are two claims, and only the first is this one's: New still asks the
	// configuration which transport a mode would use, so a web composition names a
	// constructor for its transport even though Start never calls it.
	transport events.Transport

	// working makes Work one-per-Runtime: a second scheduler on this connection
	// would run every module job twice.
	working   atomic.Bool
	closeOnce sync.Once
	closeErr  error

	// closed records that Close has run. Work reads it before it claims anything, so
	// a Work that *starts* after Close returned is refused instead of scheduling jobs
	// and a relay onto a pool nobody can reach any more. It is a state read, not a
	// shutdown mechanism: a Work already inside the scheduler is stopped by its own
	// context, and stopping the served requests and that Work before Close is the
	// caller's sequence to get right.
	closed atomic.Bool
}

// Start migrates as the owner role, opens the application connection, builds the
// API, runs every boot gate, and opens the transport the role names — in that
// order, the order Run uses. Every failure returns a nil Runtime, nothing
// listening, and everything it opened already released.
//
// The caller then owns the port: mount Handler, decide which started
// compositions also Work, and Close when both have stopped.
//
// Call Start once per App. The connection it opens, and the transport this role
// selects, belong to the Runtime it returns, and Close is the only release of
// either — including a transport the application injected in Options.Transport,
// which Close releases like one Start built. A second Start on the same App
// migrates again and opens a second connection over the same configuration, and
// an injected transport is the same instance both Runtimes then share, so closing
// either releases what the other is still using. A new lifecycle needs a new App.
func (a *App) Start(ctx context.Context) (*Runtime, error) {
	if err := a.migrate(ctx); err != nil {
		return nil, err
	}
	conn, err := a.openConn(ctx)
	if err != nil {
		return nil, err
	}
	built, err := a.buildAPI(ctx, conn)
	if err != nil {
		_ = conn.Close() // a composition that failed a gate is never mounted
		return nil, err
	}
	rt := &Runtime{app: a, conn: conn, handler: built.router}
	if a.opts.Role == Worker {
		// The routes were built and gated above and are then set aside: the two
		// probes are the whole surface Run gives a worker today.
		rt.handler = a.probes(conn)
	}
	if a.opts.Role != Web {
		if rt.transport, err = a.transport(); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	return rt, nil
}

// Handler is this composition's HTTP surface, role by role: for web and all the
// whole API — every module's routes, its static trees and the two probes, on the
// router httpx built for it — and for worker the two probes and nothing else,
// which is all Run has ever given a worker. It answers at the root, not under a
// prefix: two compositions share a listener by being chosen between above their
// routers, never by mounting both into one. Which Host arrives here is the
// caller's decision; the kernel names no host list for it.
func (r *Runtime) Handler() http.Handler { return r.handler }

// Work is the worker half — the outbox relay, the outbox purge, every module's
// jobs and every module's subscriptions. It takes no address and serves nothing,
// and returns nil when ctx is done. The transport is Start's, released by Close.
//
// Work refuses a role that composes no worker half, refuses a Runtime that Close
// has already released, and refuses a second call: the Runtime owns one
// connection, and a scheduler is not additive. Stopping the served requests and
// the earlier Work before Close stays the caller's; the closed refusal is the
// refusal of a mistake, not a way to stop anything.
//
// One composition works one database and one transport. Two of them over one
// database is not a supported arrangement — events.Relay claims any unpublished
// outbox row and stamps what it publishes, so one composition can move another's
// event onto a transport nobody subscribes to, and two schedulers over one
// database silence each other's same-named job through the advisory lock. Each
// composition keeps its own database, as one process per client does today.
func (r *Runtime) Work(ctx context.Context) error {
	if r.transport == nil {
		return errors.New("app: Work needs a transport and this Runtime has none: role web composes no worker half, and Start refuses any other role whose transport constructor answers with no transport")
	}
	if r.closed.Load() {
		return errors.New("app: Work needs a Runtime that has not been closed; Close released the connection and the transport this half works through")
	}
	if !r.working.CompareAndSwap(false, true) {
		return errors.New("app: Work is already running for this Runtime; Close it, which releases its transport, and Start a new App")
	}
	return r.app.work(ctx, r.conn, r.transport, nil)
}

// Close releases the transport, then the connection, and flushes what the tracer
// still holds, and is safe to call more than once: the second call returns the
// first call's result. Call it once the served requests and the work have stopped
// — an in-flight handler holds a detached transaction on this pool, and a running
// Work keeps its ticks until its own context is done. Work started after this
// returns is refused: what Close released — the connection, and the transport
// whether Start built it or the application injected it — is not reusable, so a new
// lifecycle starts from a new App rather than from this Runtime again.
//
// The flush is here because Close is the only teardown a caller that owns its
// listener has. Run flushes because Run is told when the process ends; this path is
// not Run, and a process that started and stopped inside one batch interval would
// otherwise keep none of the trace it recorded. It runs last, after the connection
// is released, in the order Run leaves the same act in, and its failure is logged
// rather than returned: a span nobody read is not a shutdown that failed to finish.
func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		r.closed.Store(true)
		if closer, ok := r.transport.(io.Closer); ok {
			r.closeErr = closer.Close()
		}
		if err := r.conn.Close(); r.closeErr == nil {
			r.closeErr = err
		}
		// Not the caller's context, whatever it is by now: Close is called from
		// shutdown paths whose context is already cancelled, and the flush needs
		// the same bounded grace Run gives it rather than an immediate deadline.
		grace, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		r.app.flushTraces(grace)
	})
	return r.closeErr
}
