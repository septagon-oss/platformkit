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
// mounts something, no address names a prefix, and every registration of the
// modules' Routes callbacks mounted the same surface — over a recorder, and opens
// nothing: no pool, no migration, no listener, no broker, and no connection to
// the store the deployment names (see the body for the store it does build, and
// why that one opens nothing). It registers the routes three times and runs the
// same gates over each, which is why a composition refused here is the same
// composition Start would have refused, answered while nothing has been spent.
//
// Call it before Start. Start answers all of it itself for a caller that does not,
// so the order is a caller's convenience and not a safety condition. What Start adds
// to what this answers is the fourth registration — the one whose surface would
// serve — and it is answered in the same place this one is, over an in-process store
// with no connection open, because the composition that mounts differently on its
// last registration is a composition too, and a refusal of it that first dialed the
// deployment's own database would be a refusal that spent what it says it did not.
func (a *App) Declarations() error {
	// The recorder needs a store because httpx requires one — a belief about which
	// tenant a host is has to be something every replica can forget — and this
	// composition serves nothing, so the store it is handed is named for the option
	// and thrown away with the recorder. The shared one is built by Start: dialing a
	// server the deployment named would be an effect, and this method's whole claim
	// is that it answers before any effect happens. A Routes callback sees almost
	// none of the difference: httpx.Surfaces hands back the composition's recorded
	// shape, a SystemToken — a capability, not a connection — and InvalidateHost,
	// which moves this pass's own store. Nothing on it queries the pool.
	segment, err := a.cacheSegment()
	if err != nil {
		return err
	}
	// Three times, and the second and third are the reason there are three. Every
	// other gate below reads one registration and answers about it; the agreement
	// between registrations can only be answered by registering more than once. Two
	// passes are what a callback behind an idempotent mount guard needs; a third is
	// what a callback that answers differently on its third call needs, and it costs
	// the same recorder nothing. The fourth registration — the API Start builds to
	// serve — is judged here too, by the same comparison, and for the same price:
	// composeRoutes builds it no differently, so a boot whose last registration
	// disagrees with its first is refused before it has opened anything. The first
	// pass records the standard and every later one is judged against it; see
	// registrationsAgree.
	for pass := 1; pass <= 3; pass++ {
		dry := cache.Memory(segment)
		_, _, err := a.composeRoutes(dry)
		// Each recorder store is released as soon as its recorder has answered:
		// nothing serves the API either pass returns, and a belief carried from the
		// first pass into the second would be a third thing no module mounted.
		_ = dry.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// Start answers the composition's route gates over three dry registrations, builds
// the API it will serve and runs every boot gate over it, opens the application
// connection, opens the store the deployment names and hands both to that API,
// migrates as the owner role, and opens the transport the role names — in that
// order, the order Run uses. The gates come before the connection because a gate is
// an answer about the composition and a connection is the first thing a deployment
// spends: the composition somebody has to fix is cheaper to refuse while the store
// it was pointed at is still undialed, and that includes the composition whose
// modules mount one surface when they are asked what they would serve and another
// when they are actually asked. The migration comes after the gates for the same
// reason one gate down. Nothing serves before the migration, so a
// composition that reaches a listener has its schema. Every failure returns a nil
// Runtime, nothing listening, and everything it opened already released. The one
// thing a successful Start leaves behind in the process is the composition's
// declared event shapes, put up as its last act — see below.
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
	// The route gates first, because every registration they are read from needs
	// nothing and the comparison between them can be answered no other
	// way: a boot that opens the pool and dials the shared store before it has
	// compared its registrations has spent what its refusal says it did not spend.
	// A caller that already asked — pkit.Build, which says so in its own words —
	// is not asked again; what is left to answer is the fourth registration below.
	if a.declaredRoutes == nil {
		if err := a.Declarations(); err != nil {
			return nil, err
		}
	}
	// The API this process will serve, built the way the three registrations above
	// were: over an in-process store this build never writes, and with no connection
	// open. Nothing a module's Routes callback can reach reads either of them, so the
	// registration whose surface would serve answers the gates on the same terms as
	// the three that set the standard, and a composition that means two things is
	// refused with the deployment's own store still undialed. The in-process store is
	// released below, the moment the deployment's takes over: a host resolution
	// believed by a build that may still be refused is a belief nobody serving this
	// installation should hold.
	segment, err := a.cacheSegment()
	if err != nil {
		return nil, err
	}
	build := cache.Memory(segment)
	api, handler, err := a.buildAPI(ctx, build)
	if err != nil {
		_ = build.Close() // a composition that failed a gate is never mounted
		return nil, err
	}
	// The first two things a deployment spends, and they are spent only once every
	// answer about this composition has been given.
	conn, err := a.openConn(ctx)
	if err != nil {
		_ = build.Close()
		return nil, err
	}
	store, err := a.cache(ctx)
	if err != nil {
		_ = conn.Close()
		_ = build.Close()
		return nil, err
	}
	// The router built above now holds the deployment: the connection its request
	// transactions open on and the store its host resolutions are believed in. It
	// could not have been built with them without spending them first, and nothing
	// reached it in the meantime — no listener is open and no Runtime has been handed
	// out — so this is a composition finishing itself, not a router changing under a
	// request. The three doors of this package that read the connection on a request
	// — /ready, the anonymous write limit and the record of a refusal — were built
	// against the same value being there (see heldConn).
	if err := api.Connect(conn, store); err != nil {
		_ = conn.Close()
		_ = store.Close()
		_ = build.Close()
		return nil, err
	}
	a.held.fill(conn)
	_ = build.Close()
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
	// Last, and the only piece of process state this boot changes. The catalog
	// behind events.Publish is what refuses a payload that is not the one a
	// module declared, and it belongs to the process rather than to a
	// composition, so putting it up here rather than in New is the difference
	// between two claims: "a build this one refused costs the running
	// application nothing" and "a build this one refused replaced its event
	// shapes on the way out". Everything above still refuses boots — the gates,
	// the pool, the store, the migration, the transport — and each of those
	// refusals now leaves the shapes a serving application is answering under
	// exactly where they were. Nothing between here and the end of this function
	// publishes: the first outbox row this composition can write is a request or
	// a job tick, both of which need the Runtime this call returns.
	events.DeclareAll(a.declared)
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
