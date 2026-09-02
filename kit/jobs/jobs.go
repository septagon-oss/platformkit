// Package jobs runs the periodic work: the outbox relay, the outbox purge, and
// whatever a module schedules in its manifest.
//
// It is small because it is the only background work the outbox cannot express:
// work triggered by something that happened is an event, work triggered by the
// clock is here (docs/adr/0004).
//
// Every replica in the worker role schedules every job and takes a Postgres
// advisory lock named after it before running it. Whoever gets the lock runs;
// the others log at Debug and go back to sleep. There is no leader election to
// configure, no lease to renew and no split brain: the lock lives on the
// connection that holds it, so a worker that dies releases it.
//
// A job that says Parallel skips the lock, because it has a concurrency control
// of its own. There is one, and the reason is worth stating: a locked job that
// blocks stops that job on every replica, so a job that does not need the lock
// must not take it.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Job is one piece of periodic work. Exactly one of Cron and Every says when.
type Job struct {
	// Name identifies the job in the log and, more importantly, in the
	// advisory lock: two replicas running the same job spell this the same way,
	// so it has to be stable across deployments.
	Name string
	// Cron is a five-field expression, minute first: "0 * * * *" is hourly.
	Cron string
	// Every is a fixed interval, for work that runs faster than a cron
	// expression can say. The relay uses it: once a second.
	Every time.Duration
	// Parallel says this job is safe to run on every replica at once, so the
	// scheduler does not take its lock. There is one such job — the outbox
	// relay, whose FOR UPDATE SKIP LOCKED is already the concurrency control —
	// and the reason it matters is the opposite of throughput: a job that
	// blocks while holding the lock stops that job on every other replica too,
	// so a job that does not need the lock must not take it.
	Parallel bool

	// Run is the work. It is given the worker's context, so a shutdown
	// cancels it, and the application connection, because periodic work that
	// touches nothing is periodic work with nothing to do.
	//
	// The connection is a parameter rather than something a module closes
	// over: the pool is opened by kit/app after the composition is built, so
	// at the moment a module writes this literal there is nothing to close
	// over. Passing the scheduler's own connection is also what keeps the
	// count at one — a module that opened its own would double the pool.
	Run func(ctx context.Context, conn *db.Conn) error
}

// parser is the five-field standard: minute, hour, day of month, month, day of
// week. The descriptors (@hourly, @every) are deliberately not accepted: Every
// is the field for an interval, and one way to say a thing is enough.
var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// Valid reports what is wrong with a job, or nil. kit/module checks a
// manifest's jobs with it, so a broken schedule is a composition error at boot
// rather than a job that never fires.
func Valid(j Job) error {
	switch {
	case j.Name == "":
		return errors.New("jobs: a job has no name; the name is its lock")
	case j.Run == nil:
		return fmt.Errorf("jobs: %s has nothing to run", j.Name)
	case j.Cron == "" && j.Every == 0:
		return fmt.Errorf("jobs: %s says neither Cron nor Every", j.Name)
	case j.Cron != "" && j.Every != 0:
		return fmt.Errorf("jobs: %s says both Cron %q and Every %s", j.Name, j.Cron, j.Every)
	case j.Every < 0:
		return fmt.Errorf("jobs: %s runs every %s, which is in the past", j.Name, j.Every)
	}
	if j.Cron != "" {
		if _, err := parser.Parse(j.Cron); err != nil {
			return fmt.Errorf("jobs: %s: %q is not a five-field cron expression: %w", j.Name, j.Cron, err)
		}
	}
	return nil
}

// every is a fixed interval as a cron.Schedule, so the loop below has one kind
// of schedule to ask. cron's own ConstantDelaySchedule rounds to the second,
// which a one-second relay cannot afford.
type every time.Duration

func (e every) Next(t time.Time) time.Time { return t.Add(time.Duration(e)) }

// Scheduler runs a fixed set of jobs until its context is done.
type Scheduler struct {
	conn *db.Conn
	log  *slog.Logger
	jobs []scheduled

	// The clock, so a test can run a year of ticks in a millisecond.
	now   func() time.Time
	after func(time.Duration) <-chan time.Time
}

type scheduled struct {
	job  Job
	when cron.Schedule
	next time.Time
}

// NewScheduler prepares the jobs. A malformed job panics here, at the
// construction site, because it is a wiring mistake: kit/module reports the
// same thing as a composition error before anything is constructed.
func NewScheduler(conn *db.Conn, log *slog.Logger, js ...Job) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	s := &Scheduler{conn: conn, log: log, now: time.Now, after: time.After}
	for _, j := range js {
		if err := Valid(j); err != nil {
			panic(err.Error())
		}
		when := cron.Schedule(every(j.Every))
		if j.Cron != "" {
			parsed, err := parser.Parse(j.Cron)
			if err != nil { // unreachable: Valid parsed it already
				panic(err.Error())
			}
			when = parsed
		}
		s.jobs = append(s.jobs, scheduled{job: j, when: when})
	}
	return s
}

// Run sleeps until the next job is due, runs everything that is, and repeats.
// It returns nil when ctx is done: a worker that was asked to stop has not
// failed.
//
// Jobs run one at a time, in the order they were given. The relay takes
// milliseconds and the purge runs hourly, so nothing here needs a goroutine per
// job — and a job that hangs is a job whose lock nobody else can take either,
// which is the honest consequence of "exactly one instance runs it".
func (s *Scheduler) Run(ctx context.Context) error {
	if len(s.jobs) == 0 {
		<-ctx.Done()
		return nil
	}
	now := s.now()
	for i := range s.jobs {
		s.jobs[i].next = s.jobs[i].when.Next(now)
	}
	for {
		due := s.jobs[0].next
		for _, j := range s.jobs[1:] {
			if j.next.Before(due) {
				due = j.next
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-s.after(due.Sub(s.now())):
		}
		now = s.now()
		for i := range s.jobs {
			if s.jobs[i].next.After(now) {
				continue
			}
			s.jobs[i].next = s.jobs[i].when.Next(now)
			s.run(ctx, s.jobs[i].job)
		}
	}
}

// run takes the job's lock and runs it, or reports that somebody else has it.
// A job that fails is logged and scheduled again; there is no retry of its own,
// because the next tick is the retry. A Parallel job takes no lock.
func (s *Scheduler) run(ctx context.Context, j Job) {
	if !j.Parallel {
		unlock, ok, err := db.TryLock(ctx, s.conn, "job:"+j.Name)
		if err != nil {
			s.log.ErrorContext(ctx, "jobs: could not take the lock", "job", j.Name, "error", err)
			return
		}
		if !ok {
			s.log.DebugContext(ctx, "jobs: another instance is running this", "job", j.Name)
			return
		}
		defer unlock()
	}
	if err := j.Run(ctx, s.conn); err != nil {
		s.log.ErrorContext(ctx, "jobs: job failed", "job", j.Name, "error", err)
	}
}

// TenantLister is how a job reaches every tenant. The tenant module implements
// it; nothing else can, because listing tenants is a cross-tenant read and the
// transaction that does it is minted here rather than by the implementation.
//
// It is declared in this package and not in kit/tenancy because kit/db imports
// kit/tenancy, and the signature needs both.
type TenantLister interface {
	List(ctx context.Context, tx db.Tx[db.System]) ([]tenancy.Tenant, error)
}

var listToken = syscap.NewSystemToken("list the tenants for periodic work")

// ForEachTenant runs fn once per tenant, each in that tenant's own transaction.
//
// The list is read and its transaction closed before the first tenant
// transaction opens: a tenant transaction cannot nest inside a system one, and
// holding a cross-tenant transaction open for the length of the whole job would
// be the widest lock in the system.
//
// One tenant's failure does not stop the others. Returning at the first one
// meant that a single tenant with bad data stopped every tenant after it in the
// list, on every tick, forever — and the tenants are in whatever order the
// lister returns, so which ones those were was arbitrary. Every failure is
// logged where it happened and they come back joined, so the job still reports
// as failed.
func ForEachTenant(ctx context.Context, conn *db.Conn, lister TenantLister, fn func(context.Context, db.Tx[db.Tenant]) error) error {
	if lister == nil {
		return errors.New("jobs: this job walks every tenant and the application was given no TenantLister")
	}
	var tenants []tenancy.Tenant
	err := db.RunSystem(ctx, conn, listToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		tenants, err = lister.List(ctx, tx)
		return err
	})
	if err != nil {
		return fmt.Errorf("jobs: list the tenants: %w", err)
	}
	var failed []error
	for _, t := range tenants {
		if err := db.Run(tenancy.WithTenant(ctx, t), conn, fn); err != nil {
			slog.ErrorContext(ctx, "jobs: a tenant failed; continuing with the rest",
				"tenant", t.Slug, "error", err)
			failed = append(failed, fmt.Errorf("tenant %s: %w", t.Slug, err))
		}
	}
	return errors.Join(failed...)
}
