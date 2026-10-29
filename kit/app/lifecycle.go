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

	"github.com/septagon-oss/platformkit/kit/appname"
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
	// constructor for its transport even though Start never calls it — the live door
	// may, for the connections it serves, and only the door ever does for this role
	// (heldTransport).
	transport events.Transport

	// working makes Work one-per-Runtime: a second scheduler on this connection
	// would run every module job twice.
	working   atomic.Bool
	closeOnce sync.Once
	closeErr  error

	// catalog is this boot's copy of the event shapes its composition declared, and
	// declaredRelease is the grip it holds on those names (kit/events NewCatalog).
	// Close gives the names back, which is what keeps a composition that has stopped
	// from holding its app's names against the next one — and from holding the shapes
	// the next one means. Every request and every unit of work this Runtime runs is
	// measured against the value rather than against whatever the process last
	// installed under this composition's slug.
	catalog         *events.Catalog
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
// changes nothing while the grip is in hand: New takes it and Start moves it to the
// Runtime, so a caller that starts an App whose last Start was refused, or whose last
// Runtime was Closed, takes it again rather than serving publishes the process-wide record
// no longer measures. The guard reads the grip and not the contract value beside it because
// the two are separate facts: the value says what was composed and outlives any one boot,
// the grip says this process is answering under it.
func (a *App) claimDeclared() error {
	if a.declaredRelease != nil {
		return nil
	}
	catalog, err := events.NewCatalog(a.opts.App, a.declared)
	if err != nil {
		return err
	}
	a.catalog, a.declaredRelease = catalog, catalog.Close
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
	a.catalog = nil
}

// takeDeclaredRelease moves the grip to the Runtime this Start returns, whose Close is
// then the release of it. A Runtime nobody Closes keeps its app's names claimed, which is
// the same answer it gives its connection. The contract itself stays with the composition:
// it is a fact about what was composed, not about whether a pool is still open.
func (a *App) takeDeclaredRelease() func() {
	release := a.declaredRelease
	a.declaredRelease = nil
	return release
}

// Publisher is this composition's event door: the value that measures every publish it
// carries against this composition's own manifest (kit/events Publisher). It is what a
// composition hands to a module that names an events.Publisher in its own Deps, and to a
// caller that publishes from outside a served request or a scheduled job — a seed, a CLI
// command, a goroutine that started from nowhere. A composition whose constructor was
// refused has none, and never got this far.
func (a *App) Publisher() events.Publisher { return a.publisher() }

// Publisher is this started Runtime's composition's event door. See App.Publisher; a
// Runtime that has been Closed hands the same value out, and the resources that value's
// boot released answer the publish rather than a second refusal.
func (r *Runtime) Publisher() events.Publisher {
	if r.catalog == nil {
		return nil
	}
	return r.catalog
}

// publisher answers the same question for the inside of the package, where a nil
// composition must not arrive as a non-nil interface.
func (a *App) publisher() events.Publisher {
	if a.catalog == nil {
		return nil
	}
	return a.catalog
}

// Declarations validates three identical route registrations using temporary
// in-process caches. It opens no deployment resources and releases the event
// claim on refusal. Start calls it if needed, then checks the serving registration.
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
		dry := cache.MemoryFor(appname.Name(segment))
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

// Start gates every registration before opening the pool and cache, connecting
// the API, migrating, and opening the role's transport. Failure releases all
// opened resources and returns a nil Runtime; RefusedBeforeEffects identifies
// failures before deployment resources opened. Telemetry installs only on success.
// Call once per App. The caller owns serving Handler, running Work, and stopping
// both before Close, which also releases injected transports and flushes telemetry.
func (a *App) Start(ctx context.Context) (*Runtime, error) {
	api, handler, build, err := a.prepareRuntime(ctx)
	if err != nil {
		return nil, err
	}
	conn, store, err := a.connectRuntime(ctx, api, build)
	if err != nil {
		return nil, err
	}
	return a.finishRuntime(ctx, api, conn, store, handler)
}

// Handler is this composition's HTTP surface, role by role: for web and all the
// whole API — every module's routes, its static trees and the two probes, on the
// router httpx built for it — and for worker the two probes and nothing else,
// which is all Run has ever given a worker. It answers at the root, not under a
// prefix: two compositions share a listener by being chosen between above their
// routers, never by mounting both into one. Which Host arrives here is the
// caller's decision; the kernel names no host list for it.
func (r *Runtime) Handler() http.Handler { return r.handler }

// Work runs the relay, purge, jobs and subscriptions until ctx ends, then returns
// nil. It serves no HTTP and refuses web-only, closed or already-started runtimes.
// Stop requests and Work before Close. Each composition needs its own database
// and transport so relay claims and scheduler locks cannot affect another app.
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
// released — the connection, the cache, and the transport whether Start built it,
// the application injected it or this Runtime's live door dialled it — is not
// reusable, so a new lifecycle starts from a new App rather than from this Runtime
// again.
func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		r.closed.Store(true)
		if closer, ok := r.transport.(io.Closer); ok {
			r.closeErr = closer.Close()
		}
		// The transport a live connection watched is released beside this Runtime's
		// own, and only when the door dialled it: a role with a worker half was just
		// closed above, and a role without one holds nothing but what it dialled
		// itself (heldTransport).
		if err := r.app.observed.release(); r.closeErr == nil {
			r.closeErr = err
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
