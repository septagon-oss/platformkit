package jobs

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// clock is a scheduler's whole view of time. After advances it by exactly the
// interval it was asked to wait and returns immediately, so a test runs a day of
// ticks in a millisecond and asserts the times the jobs believed they ran at.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	if d > 0 {
		c.at = c.at.Add(d)
	}
	now := c.at
	c.mu.Unlock()
	ch := make(chan time.Time, 1)
	ch <- now
	return ch
}

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

// unique keeps two packages' tests from contending for one advisory lock: the
// locks are per database, and the test schemas are not.
func unique(name string) string { return name + "-" + uuid.NewString() }

// TestBothKindsOfScheduleFire: an interval for work that runs faster than cron
// can say, and a five-field expression for everything else.
func TestBothKindsOfScheduleFire(t *testing.T) {
	_, conn := dbtest.Schema(t)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &clock{at: start}

	var (
		mu   sync.Mutex
		fire []string
	)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	record := func(name string) func(context.Context) error {
		return func(context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			if len(fire) < 13 {
				fire = append(fire, name+" "+c.Now().Format("15:04"))
			}
			if len(fire) == 13 {
				stop()
			}
			return nil
		}
	}
	s := NewScheduler(conn, quiet(),
		Job{Name: unique("fast"), Every: 5 * time.Minute, Run: record("fast")},
		Job{Name: unique("hourly"), Cron: "0 * * * *", Run: record("hourly")},
	)
	s.now, s.after = c.Now, c.After
	if err := s.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(fire) < 13 {
		t.Fatalf("only %d runs: %v", len(fire), fire)
	}
	// Twelve five-minute ticks reach 01:00, where the hourly job joins them.
	for i, want := range []string{
		"fast 00:05", "fast 00:10", "fast 00:15", "fast 00:20", "fast 00:25", "fast 00:30",
		"fast 00:35", "fast 00:40", "fast 00:45", "fast 00:50", "fast 00:55", "fast 01:00",
		"hourly 01:00",
	} {
		if fire[i] != want {
			t.Errorf("run %d was %q, want %q (all: %v)", i, fire[i], want, fire)
		}
	}
}

// TestOnlyOneSchedulerRunsAJob is the leader election, and it is one advisory
// lock: while one instance holds it, another asked to run the same job does
// nothing and says so at Debug.
func TestOnlyOneSchedulerRunsAJob(t *testing.T) {
	_, conn := dbtest.Schema(t)
	name := unique("exclusive")

	started := make(chan struct{})
	release := make(chan struct{})
	var runs sync.WaitGroup
	var mu sync.Mutex
	count := 0
	job := Job{Name: name, Every: time.Hour, Run: func(context.Context) error {
		mu.Lock()
		count++
		mu.Unlock()
		started <- struct{}{}
		<-release
		return nil
	}}

	first := NewScheduler(conn, quiet(), job)
	second := NewScheduler(conn, quiet(), job)

	runs.Add(1)
	go func() {
		defer runs.Done()
		first.run(t.Context(), job)
	}()
	<-started // the first instance is inside the job, holding the lock

	second.run(t.Context(), job) // returns at once, having run nothing
	mu.Lock()
	if count != 1 {
		t.Errorf("the job ran %d times while one instance held the lock, want 1", count)
	}
	mu.Unlock()

	release <- struct{}{}
	runs.Wait()

	// And once the lock is free the other instance can take it.
	runs.Add(1)
	go func() {
		defer runs.Done()
		second.run(t.Context(), job)
	}()
	<-started
	release <- struct{}{}
	runs.Wait()

	mu.Lock()
	defer mu.Unlock()
	if count != 2 {
		t.Errorf("the job ran %d times in total, want 2", count)
	}
}

// lister is what the tenant module implements in E3.
type lister []tenancy.Tenant

func (l lister) List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) {
	return l, nil
}

// TestForEachTenantRunsInEachTenantsOwnTransaction: a job that walks tenants
// gets the same Tx[Tenant] a request handler gets, one tenant at a time, so
// row-level security applies to it exactly as it does to a request.
func TestForEachTenantRunsInEachTenantsOwnTransaction(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenants := lister{
		{ID: uuid.New(), Slug: "acme"},
		{ID: uuid.New(), Slug: "globex"},
	}

	var seen []string
	err := ForEachTenant(t.Context(), conn, tenants, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var setting string
		// The tenant is on the transaction as far as Postgres is concerned,
		// which is the only place it counts.
		if err := tx.DB().Raw("SELECT platformkit_current_tenant_id()::text").Scan(&setting).Error; err != nil {
			return err
		}
		if setting != db.TenantOf(tx).ID.String() {
			t.Errorf("the transaction says %s and the handle says %s", setting, db.TenantOf(tx).ID)
		}
		seen = append(seen, db.TenantOf(tx).Slug)
		return nil
	})
	if err != nil {
		t.Fatalf("ForEachTenant: %v", err)
	}
	if got := strings.Join(seen, " "); got != "acme globex" {
		t.Errorf("visited %q, want every tenant in order", got)
	}

	// A job that needs the tenants and was given none says so, rather than
	// doing nothing and reporting success.
	err = ForEachTenant(t.Context(), conn, nil, func(context.Context, db.Tx[db.Tenant]) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "TenantLister") {
		t.Errorf("ForEachTenant with no lister = %v", err)
	}
}

// TestValidRefusesAJobThatCouldNotRun. Every one of these is a composition
// error kit/module reports at boot rather than a job that silently never fires.
func TestValidRefusesAJobThatCouldNotRun(t *testing.T) {
	run := func(context.Context) error { return nil }
	for what, job := range map[string]Job{
		"no name":             {Every: time.Minute, Run: run},
		"nothing to run":      {Name: "a", Every: time.Minute},
		"no schedule":         {Name: "a", Run: run},
		"two schedules":       {Name: "a", Cron: "* * * * *", Every: time.Minute, Run: run},
		"a negative interval": {Name: "a", Every: -time.Minute, Run: run},
		"six fields":          {Name: "a", Cron: "0 0 * * * *", Run: run},
		"a descriptor":        {Name: "a", Cron: "@hourly", Run: run},
	} {
		if err := Valid(job); err == nil {
			t.Errorf("Valid accepted %s", what)
		}
	}
	for _, job := range []Job{
		{Name: "a", Every: time.Second, Run: run},
		{Name: "b", Cron: "*/5 * * * *", Run: run},
	} {
		if err := Valid(job); err != nil {
			t.Errorf("Valid(%s) = %v", job.Name, err)
		}
	}
}
