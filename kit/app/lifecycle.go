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

	// declaredRelease is this boot's receipt for the event shapes its composition
	// claimed in its own app's catalog (kit/events ClaimApp). Close gives them back,
	// which is what keeps a composition that has stopped from holding its app's names
	// against the next one — and from holding the shapes the next one means.
	declaredRelease func()

	// closed records that Close has run. Work reads it before it claims anything, so
	// a Work that *starts* after Close returned is refused instead of scheduling jobs
	// and a relay onto a pool nobody can reach any more. It is a state read, not a
	// shutdown mechanism: a Work already inside the scheduler is stopped by its own
	// context, and stopping the served requests and that Work before Close is the
	// caller's sequence to get right.
	closed atomic.Bool
}

// refusedBeforeEffects marks the refusals a boot answers before the deployment
// pays for anything: the cache segment the configuration names, the route gates,
// the three dry registrations, the fourth registration whose surface would serve
// and the claim on the event shapes this composition declares — all answers about
// the composition, given over an in-process store with no pool dialled, nothing
// migrated, nothing listened on and nothing served. Everything from openConn
// onward — the connection, the store the deployment names, the migration and the
// transport — is spent, even when the boot gives back what it opened.
//
// The line belongs to kit/app because kit/app is where the deployment starts to
// pay, and nothing in the error's text says which side of it a refusal came from.
// A caller that records having spent itself on the way to a boot — pkit.App.Build
// records exactly that, because one App is one lifecycle — can only keep the
// record honest by asking where the refusal was answered.
type refusedBeforeEffects struct{ error }

func (e refusedBeforeEffects) Unwrap() error { return e.error }

// RefusedBeforeEffects reports whether err is a boot refusal kit/app answered
// while the deployment was still untouched: no pool dialled, no shared store
// dialled, nothing migrated and nothing listening. A caller that spent a lifecycle
// on the way to such a boot spent nothing and may begin again with a corrected
// composition; any other refusal reached the deployment, and whatever else the
// released boot gave back, that lifecycle is among what it spent.
func RefusedBeforeEffects(err error) bool {
	var refused refusedBeforeEffects
	return errors.As(err, &refused)
}

// beforeEffects marks an error as answered on the free side of the line above.
func beforeEffects(err error) error { return refusedBeforeEffects{err} }

// claimDeclared takes this composition's grip on its own app's event shapes, and
// changes nothing when the grip is already held: New takes it, and a caller that
// starts an App whose last Start was refused takes it again rather than serving a
// composition whose payloads nothing measures.
func (a *App) claimDeclared() error {
	if a.declaredRelease != nil {
		return nil
	}
	release, err := events.ClaimApp(a.opts.App, a.declared)
	if err != nil {
		return err
	}
	a.declaredRelease = release
	return nil
}

// giveBackDeclared hands the grip back. A refusal after New leaves no shape of a
// composition that never started standing in its app's catalog: the names it
// declared are free again, which is what lets the corrected composition that fixes
// them claim them its own way. Both boot doors answer every refusal with it, the
// free side of the line included, because a refusal is the one outcome that never
// hands over a Runtime to give the grip back later.
func (a *App) giveBackDeclared() {
	if a.declaredRelease == nil {
		return
	}
	a.declaredRelease()
	a.declaredRelease = nil
}

// takeDeclaredRelease moves the grip from the composition to the Runtime this Start
// returns, whose Close is then the release of it. A Runtime nobody Closes keeps its
// app's names claimed, which is the same answer it gives its connection.
func (a *App) takeDeclaredRelease() func() {
	release := a.declaredRelease
	a.declaredRelease = nil
	return release
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
		a.giveBackDeclared() // no Runtime is coming to give back New's grip
		return beforeEffects(err)
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
			// The changed registration is a refusal of the composition, and the
			// caller that corrects it has to find its own event names free beside it.
			a.giveBackDeclared()
			return beforeEffects(err)
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
// Runtime, nothing listening, and everything it opened already released. Which of
// those failures cost a caller its own lifecycle is what RefusedBeforeEffects
// answers: a refusal above the connection was answered with nothing spent, and one
// below it reached the deployment. The two pieces of process state a boot touches
// are the composition's declared event shapes, claimed by New on its last line and
// given back by the refusal that follows or the Close of the Runtime it made (see
// below, because where the claim sits is the whole answer about what a refused
// composition costs), and the process's telemetry providers, installed by the last
// statement of a Start that had nothing left to refuse, so a boot that comes back
// with no Runtime changed no process but its own.
//
// The caller then owns the port: mount Handler, decide which started
// compositions also Work, and Close when both have stopped.
//
// Call Start once per App. The connection it opens, and the transport this role
// selects, belong to the Runtime it returns, and Close is the only release of
// either — including a transport the application injected in Options.Transport,
// which Close releases like one Start built.
//
// The flush is here because Close is the only teardown a caller that owns its
// listener has. Run flushes because Run is told when the process ends; this path is
// not Run, and a process that started and stopped inside one export interval would
// otherwise keep none of the trace it recorded. It runs last, after the connection is
// released, in the order Run leaves the same act in, and its failure is logged rather
// than returned: spans nobody read are not a shutdown that failed to finish. A second Start on the same App
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
		// Already marked by Declarations: every answer it gives is about the
		// composition and costs the deployment nothing.
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
		a.giveBackDeclared() // no Runtime on this side either
		return nil, beforeEffects(err)
	}
	build := cache.Memory(segment)
	api, handler, err := a.buildAPI(ctx, build)
	if err != nil {
		_ = build.Close() // a composition that failed a gate is never mounted
		// The fourth registration — the surface that would serve — is answered over
		// that in-process store, so its refusal has spent nothing but the grip New
		// took, and the grip goes back with the store it was refused over.
		a.giveBackDeclared()
		return nil, beforeEffects(err)
	}
	// The composition's event shapes were claimed by New, above the connection, and
	// they are what this boot either keeps or gives back. The catalog behind
	// events.Publish is what refuses a payload that is not the one a module declared,
	// and it is keyed by app, so two claims hold at once: a build this process refused
	// leaves the shapes another app answers under exactly where they were, and a
	// refusal about those shapes costs the database it named nothing. New answers the
	// disagreement already visible when a boot starts — a name this app already chose
	// another way — and takes the claim on its last line, so the boot that reaches the
	// claim second is refused with its pool undialed, its schema unmigrated and its
	// own composition free to be corrected and built again.
	//
	// Beside, not over: another composition can be live in this process — one
	// application under two roles, or two applications on two databases — and what a
	// publish is checked against is then that app's own declarations, counted by grip
	// and given back by the Close of the Runtime that took it. A caller that starts
	// this App again after a refusal takes the grip again here rather than serving a
	// composition whose payloads nothing measures.
	if err := a.claimDeclared(); err != nil {
		_ = build.Close()
		return nil, beforeEffects(err)
	}
	// The first two things a deployment spends, and they are spent only once every
	// answer about this composition has been given — this claim among them.
	conn, err := a.openConn(ctx)
	if err != nil {
		a.giveBackDeclared()
		_ = build.Close()
		return nil, err
	}
	store, err := a.cache(ctx)
	if err != nil {
		a.giveBackDeclared()
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
		a.giveBackDeclared()
		_ = conn.Close()
		_ = store.Close()
		_ = build.Close()
		return nil, err
	}
	a.held.fill(conn)
	_ = build.Close()
	if err := a.migrate(ctx); err != nil {
		a.giveBackDeclared()
		_ = conn.Close()
		_ = store.Close()
		return nil, err
	}
	rt := &Runtime{app: a, conn: conn, handler: handler, cache: store,
		declaredRelease: a.takeDeclaredRelease()}
	if a.opts.Role == Worker {
		// The routes were built and gated above and are then set aside: the two
		// probes are the whole surface Run gives a worker today.
		rt.handler = a.probes(api, conn)
	}
	if a.opts.Role != Web {
		if rt.transport, err = a.transport(); err != nil {
			// Close, and not the two fields by hand: this Runtime holds the claim
			// this boot took, and a refusal that released everything else and kept
			// the shapes would leave them named by a composition that never started.
			_ = rt.Close()
			return nil, err
		}
	}
	// The process's providers go up here, as the boot's last act and the only thing
	// it changes in the process for good. Everything above this line can still be
	// refused — a gate, the pool, the store, the schema, a broker — and every refusal
	// above it returns a nil Runtime, which README says changes nothing in the process
	// it was asked in. Installing is the one effect no Close gives back: a pool is
	// closed, a claim is released, a replaced tracer provider is not reinstated, only
	// overwritten by the next boot. So a composition that never started never takes
	// over the export destination of the application that is serving.
	a.measurement.install(ctx)
	// The claim this boot took is the Runtime's to give back, after its connection,
	// its transport and whatever else this call opened: a refusal between the claim
	// and here released it on the way out, so this is the only path that keeps one.
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

// Close releases the transport, the connection, the event shapes this composition
// declared, then the cache, then flushes what the exporters still hold, and is safe
// to call more than once: the second call returns the first call's result. Call it
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
		// The declared shapes go back after the connection, not before it: until the
		// pool is closed a handler this Runtime served could still be publishing, and
		// giving the guard away early is how a mis-shaped payload slips past a boot
		// that had promised to refuse it. What goes back is this Runtime's own
		// declarations, and only the ones no other live composition named as well.
		if r.declaredRelease != nil {
			r.declaredRelease()
		}
		// Last of the resources, because nothing depends on it and a store that
		// refused its own release must not hide a pool that refused one first. A
		// cache is a belief; the truth it was about is in the database.
		if err := r.cache.Close(); r.closeErr == nil {
			r.closeErr = err
		}
		// And last of everything, because the flush is what the process still owes
		// the collector rather than a resource it holds: not the caller's context,
		// whatever it is by now — Close is called from shutdown paths whose context
		// is already cancelled, and the flush needs the same bounded grace Run gives
		// it rather than an immediate deadline.
		grace, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		r.app.flushTelemetry(grace)
	})
	return r.closeErr
}
