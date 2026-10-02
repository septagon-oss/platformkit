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

	"github.com/septagon-oss/platformkit/kit/cache"
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

	// cache is the store Start chose from the configuration — the shared one or
	// this process's own — and Close is its one release, as it is the transport's.
	// It belongs to the Runtime rather than to buildAPI's local because a value
	// every replica reads outlives the handler that reads it: the worker half
	// resolves hosts too, and an adapter closed when the router was built would
	// take that with it.
	cache cache.Cache

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

// Declarations answers the composition's route gates — every operation declared
// an authorization, no operation is guarded by a permission no composed module
// defines, no operation publishes an event no module promised, the workspace
// mounts something, no address names a prefix — over a recorder, and opens
// nothing: no pool, no migration, no listener, no broker, and no connection to
// the store the deployment names (see the body for the store it does build, and
// why that one opens nothing). It is the same registration and the same gates
// Start runs after the connection is open, which is why a composition refused
// here is the same composition Start would have refused, answered while nothing
// has been changed yet.
//
// Call it before Start. A caller that does is not unsafe, only late: Start runs
// the gates again over the live connection and refuses the boot there — before it
// migrates, and with one more answer, which is whether the routes the modules
// mounted the second time are the routes they mounted the first.
func (a *App) Declarations() error {
	// The recorder needs a store because httpx requires one — a belief about which
	// tenant a host is has to be something every replica can forget — and this
	// composition serves nothing, so the store it is handed is named for the option
	// and thrown away with the recorder. The shared one is built by Start: dialing a
	// server the deployment named would be an effect, and this method's whole claim
	// is that it answers before any effect happens.
	segment, err := a.cacheSegment()
	if err != nil {
		return err
	}
	dry := cache.Memory(segment)
	defer func() { _ = dry.Close() }()
	api, _, err := a.composeRoutes(nil, dry, true)
	if api != nil {
		// What the gates just read is what the registration on the way to serving is
		// held to; see refuseASecondRegistration.
		a.declaredRoutes = registeredRoutes(api)
	}
	return err
}

// Start opens the application connection, builds the API, runs every boot gate,
// migrates as the owner role, and opens the transport the role names — in that
// order, the order Run uses. The gates come before the migration because a gate is
// an answer about the composition and a migration is a change to the database: the
// composition somebody has to fix is cheaper to refuse while the schema it was
// pointed at is still the schema it was. Nothing serves before the migration, so a
// composition that reaches a listener has its schema. Every failure returns a nil
// Runtime, nothing listening, and everything it opened already released.
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
	conn, err := a.openConn(ctx)
	if err != nil {
		return nil, err
	}
	store, err := a.cache(ctx)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	handler, err := a.buildAPI(ctx, conn, store)
	if err != nil {
		_ = conn.Close() // a composition that failed a gate is never mounted
		_ = store.Close()
		return nil, err
	}
	if err := a.migrate(ctx); err != nil {
		_ = conn.Close()
		_ = store.Close()
		return nil, err
	}
	rt := &Runtime{app: a, conn: conn, handler: handler, cache: store}
	if a.opts.Role == Worker {
		// The routes were built and gated above and are then set aside: the two
		// probes are the whole surface Run gives a worker today.
		rt.handler = a.probes(conn)
	}
	if a.opts.Role != Web {
		if rt.transport, err = a.transport(); err != nil {
			_ = conn.Close()
			_ = rt.cache.Close()
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

// Close releases the transport, the connection, then the cache, and is safe to
// call more than once: the second call returns the first call's result. Call it
// once the served requests and the work have stopped — an in-flight handler holds a
// detached transaction on this pool, and a running Work keeps its ticks until its
// own context is done. Work started after this returns is refused: what Close
// released — the connection, the cache, and the transport whether Start built it
// or the application injected it — is not reusable, so a new lifecycle starts from
// a new App rather than from this Runtime again.
func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		r.closed.Store(true)
		if closer, ok := r.transport.(io.Closer); ok {
			r.closeErr = closer.Close()
		}
		if err := r.conn.Close(); r.closeErr == nil {
			r.closeErr = err
		}
		// Last, because nothing depends on it and a store that refused its own
		// release must not hide a pool that refused one first. A cache is a belief;
		// the truth it was about is in the database.
		if err := r.cache.Close(); r.closeErr == nil {
			r.closeErr = err
		}
	})
	return r.closeErr
}
