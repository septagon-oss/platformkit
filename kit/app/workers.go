package app

import (
	"context"
	"net/http"
	"time"

	"github.com/septagon-oss/platformkit/kit/appname"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/limit"
)

// The built-in jobs, in every worker. The relay is a second because an event is
// asynchronous, not slow; the two purges are hourly because a week of history and
// a day of closed windows do not need attention more often than that.
const (
	relayEvery = time.Second
	purgeCron  = "0 * * * *"
	// relayTimeout bounds one relay pass. A pass is milliseconds of work; five
	// seconds means the transport is not taking events, and the honest response
	// to that is to stop and let the other jobs run.
	relayTimeout = 5 * time.Second
)

// kernelJobs is the periodic work the composition owns because no module does: the
// outbox relay and the three tables the kernel writes that nothing but time makes
// smaller. A module's own jobs are appended to this list by work; nothing here
// reaches a module's table.
func kernelJobs(transport events.Transport, app appname.Name, clocks ...func() time.Time) []jobs.Job {
	now := db.Now
	if len(clocks) > 0 && clocks[0] != nil {
		now = clocks[0]
	}
	list := []jobs.Job{
		// Parallel, because SKIP LOCKED is already the concurrency control, and
		// bounded, because a transport that blocks would otherwise hold the
		// scheduler — and with it the purges and every module's job — for as long
		// as it blocked. A pass that runs out of time leaves its rows unstamped and
		// the next tick takes them.
		{Name: "outbox-relay", Every: relayEvery, Parallel: true, Run: func(ctx context.Context, conn *db.Conn) error {
			ctx, cancel := context.WithTimeout(ctx, relayTimeout)
			defer cancel()
			return events.RelayApp(ctx, conn, transport, app)
		}},
		{Name: "outbox-purge", Cron: purgeCron, Run: func(ctx context.Context, conn *db.Conn) error {
			return events.Purge(ctx, conn)
		}},
	}
	// Retry ledger movement under the scheduler's advisory lock to catch rows
	// written after boot. Only named applications have scoped ledgers to move.
	if app.Named() {
		list = append(list, jobs.Job{Name: "ledger-move", Every: ledgerMoveEvery,
			Run: func(ctx context.Context, conn *db.Conn) error {
				_, err := events.MoveLedger(ctx, conn, app, "job:ledger-move")
				return err
			}})
	}
	// The counters table is written by whoever holds a limiter, and this
	// composition is one of them: httpx counts anonymous public writes on the
	// limiter the runner hands it. kit/limit wrote the condition for this line in
	// advance — "the moment a second module adopts this, the purge belongs beside
	// the outbox's" — and the condition arrived with the public write limit. The
	// last field of every key here is an address a caller chooses, so a table
	// nobody empties grows at somebody else's rate; leaving the purge in
	// `modules/auth`'s sweep would have made an installation that takes the limit
	// and not the login page one that never forgets a row.
	list = append(list, jobs.Job{Name: "limit-purge", Cron: purgeCron, Run: limit.Purge})
	// The same argument one row over, and the same schedule: a record of a keyed
	// write holds that caller's own request and answer, and the day the brief
	// gives it is enforced by this job and nothing else. Leaving it to the hot
	// path would make a refusal pay for the traffic it is refusing, and leaving
	// it out entirely would make the table one anybody can grow.
	return append(list, jobs.Job{Name: "idempotency-purge", Cron: purgeCron, Run: func(ctx context.Context, conn *db.Conn) error { return idempotency.PurgeAt(ctx, conn, now()) }})
}

// ledgerMoveEvery is how often the worker runs the delivery-ledger move on the
// scheduler. The act is idempotent and does nothing once the ledger is scoped,
// so this is the price of not having to know when to stop: long enough that a
// mid-claim delivery has certainly finished (handlerTimeout is 25 s), short
// enough that a claim an unscoped writer adds between two boots is renamed soon.
const ledgerMoveEvery = 2 * time.Minute

// ledgerMoveRetry is how long the worker waits before it asks again for a move a
// mid-claim delivery refused, and it is shorter than the retry job's cadence
// because the cause is bounded: events.handlerTimeout says a handler holds the
// durable for at most 25 s, so a refusal is a delivery that is nearly finished,
// not a condition an operator has to be woken for. A deployment waits this long,
// at most, between being told to start and serving its tenants' events.
const ledgerMoveRetry = 5 * time.Second

// moveLedger retries this application's ledger rename until it succeeds or
// ctx ends. Subscriptions wait here so scoped consumers cannot replay work
// already recorded under the old durable. Unnamed applications move nothing.
func (a *App) moveLedger(ctx context.Context, conn *db.Conn) error {
	for {
		report, err := events.MoveLedger(ctx, conn, a.opts.App, "boot")
		if err == nil {
			if report.Claims != 0 || report.Dead != 0 {
				a.log.InfoContext(ctx, "app: the delivery ledger moved", "app", a.opts.App.String(),
					"subscriptions", report.Subscriptions, "claims", report.Claims,
					"dead_letters", report.Dead, "tenants", report.Tenants)
			}
			return nil
		}
		// A stop is a stop: the refusal that ends this loop is the context, not
		// the database, and a process told to leave returns nothing an orchestrator
		// would read as a crash.
		if ctx.Err() != nil {
			return nil
		}
		a.log.WarnContext(ctx, "app: the delivery ledger did not move, holding its subscriptions until it does",
			"app", a.opts.App.String(), "retry", ledgerMoveRetry.String(), "error", err)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(ledgerMoveRetry):
		}
	}
}

// drainMigrations is the worker's schedule for the data migrations this
// composition owns. It is built here rather than inside kernelJobs because it
// needs three facts the transport does not carry: the owner-role URL migrations run
// under, the budgets the configuration named, and the sources the composition
// actually selected. Without it the drain that Migrate deliberately leaves behind
// would be left behind forever, and "resumable" would mean "somebody runs a command
// nobody has read about".
func (a *App) drainMigrations() jobs.Job {
	// The declaration travels with the tick for the reason db.BackfillWithOptions
	// gives: a placement runs where the drain runs, and the composition is the only
	// party that still knows what it named itself when the tick arrives. A
	// declaration this configuration cannot make is a refused worker, not a drain
	// that places nothing and calls it a tick — the same sentence Migrate writes
	// about the same setting, one step later.
	decl, err := migrationDeclaration(a.cfg)
	if err != nil {
		return jobs.Job{Name: jobs.BackfillJobName, Every: jobs.BackfillEvery,
			Run: func(context.Context, *db.Conn) error { return err }}
	}
	return jobs.BackfillMigrations(jobs.BackfillEvery, a.cfg.Database.MigrateURL,
		migrationBudget(a.cfg.Database), decl, MigrationSources(a.mods)...)
}

// work is the worker role: the kernel's own jobs, every module's jobs, and every
// module's subscriptions. probes is the handler it serves, or nil when the web half
// of the same process is already serving them.
func (a *App) work(ctx context.Context, conn *db.Conn, transport events.Transport, probes http.Handler) error {
	scheduled := append(kernelJobs(transport, a.opts.App, a.opts.Clock), a.drainMigrations())
	var subs []events.Subscription
	for _, m := range a.mods {
		scheduled = append(scheduled, m.Jobs...)
		for _, s := range m.Subscriptions {
			// The composition owns this fact. A subscription belongs to the app
			// that composes the module that declared it, and a module never names
			// an app — that is the deployment's, from configuration (Options.App),
			// and the same line above hands it to the job scheduler and the one
			// inside kernelJobs hands it to the relay's claim. What it names is the
			// durable: the JetStream consumer on the one stream, the deliver group
			// its replicas join, and half the key of the handled ledger (see
			// kit/appname.Durable). Left unset here, two compositions of one module
			// name one consumer, and the second app load-balances the first's
			// tenants' work into its own handlers.
			s.App = a.opts.App
			subs = append(subs, s)
		}
	}
	// The delivery context carries this composition's connection, the way the
	// transaction middleware carries it on a request (kit/httpx.transaction calls
	// httpx.WithConn for every one). A subscription is a caller in the same
	// position as a request: it has a tenant transaction open on the context, and
	// the one write that legitimately opens a second, shorter one — the fact that
	// has to be committed *before* the step that announces it, modules/auth's
	// emailed credential — reaches the pool through httpx.ConnFrom rather than
	// holding a handle of its own.
	//
	// It cannot reach it through the wiring path. The composition builds the
	// routes and answers every gate over them before this pool is dialled, so
	// a module that took a connection while its Routes callback ran took nothing,
	// and the failure only surfaced as a mail that never arrived.
	ctx = httpx.WithConn(ctx, conn)
	// The ledgers move before any subscription is made, and moveLedger is what
	// makes that ordering a fact rather than an intention: a refusal parks the
	// worker here until the rename commits, and no consumer exists while it waits.
	// Consuming over a ledger that has not moved is the double-handling window
	// kit/events/ledger.go states, widened from "as long as the move takes" to "as
	// long as the retry takes" — an event already handled keys its claim under the
	// old durable, so the first scoped delivery inserts a second claim and runs the
	// handler again before any retry tick can arrive.
	if err := a.moveLedger(ctx, conn); err != nil {
		return err
	}
	if err := events.Consume(ctx, conn, transport, subs); err != nil {
		return err
	}
	a.log.InfoContext(ctx, "app: working", "jobs", len(scheduled), "subscriptions", len(subs))

	scheduler := jobs.NewScheduler(conn, a.log, a.opts.App, scheduled...)
	if probes == nil {
		return scheduler.Run(ctx)
	}
	return a.race(ctx,
		func(ctx context.Context) error { return scheduler.Run(ctx) },
		func(ctx context.Context) error { return a.serve(ctx, probes) })
}

// both runs the web half and the worker half in one process.
func (a *App) both(ctx context.Context, conn *db.Conn, transport events.Transport, router http.Handler) error {
	return a.race(ctx,
		func(ctx context.Context) error { return a.work(ctx, conn, transport, nil) },
		func(ctx context.Context) error { return a.serve(ctx, router) })
}

// race runs two halves and returns the first failure, having stopped the other.
// Either half stopping means this process is stopping: there is no half of an
// application worth keeping alive on its own.
func (a *App) race(ctx context.Context, halves ...func(context.Context) error) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, len(halves))
	for _, half := range halves {
		go func() { done <- half(ctx) }()
	}
	first := <-done
	stop()
	for range halves[1:] {
		<-done
	}
	return first
}
