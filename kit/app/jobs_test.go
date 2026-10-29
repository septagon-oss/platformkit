package app

// The kernel writes two tables that nothing but time makes smaller, and the
// composition that writes one has to schedule the job that empties it. The reference
// application cannot show this: it composes modules/auth, whose sweep used to be the
// only purge of the counter table, so every assertion made against the running
// installation passes whether or not the kernel owns the job. What is at stake is the
// composition this repository does not build — one that takes the limit on anonymous
// public writes and not the sign-in page — whose rows would then never go, at whatever
// rate somebody who chooses the address in the key finds comfortable.

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestTheRunnerPurgesTheCounterTableItWrites. The purge moved out of
// `modules/auth`'s sweep to here for the reason kit/limit wrote down in advance, and
// this is the case that keeps it here: the job the worker actually schedules, run over
// the real table. A window that closed a day ago goes and one still counting stays,
// because a purge that emptied the live counters would be a limiter that forgets every
// hour whom it has already refused.
func TestTheRunnerPurgesTheCounterTableItWrites(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	purge := kernelJob(t, kernelJobs(memory.New(), ""), "limit-purge")

	// One spent window and one open one, both written the way a request writes one:
	// on the request's own connection, under a tenant's context, through the limiter
	// httpx is handed. The ask is repeated until the window is on the table, because
	// the answer that records nothing carries no error to notice: the first event under
	// a quota of three is admitted, so the one answer that is neither an error nor an
	// admission is the counter still being busy with this key — it reached no row and
	// counted nothing, and says so by refusing rather than by failing, which is what
	// lets a caller that fails open keep serving while a counter is queued (kit/limit's
	// Reserve answers ErrBusy this way on purpose). A helper that read only the error
	// would leave the count below to blame the job for a table the counter never
	// filled; the whole suite shares one Postgres, and the wall one counter attempt is
	// given is short next to the load several suites put on it.
	count := func(name string) {
		t.Helper()
		id := uuid.New()
		for ask := 1; ; ask++ {
			ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: id, Slug: name}), conn)
			admitted, _, _, err := limit.Postgres(httpx.ConnFrom).Reserve(ctx, name, 3, time.Minute)
			if err != nil {
				t.Fatalf("count a %s window: %v", name, err)
			}
			if admitted {
				return
			}
			if ask >= seedAsks {
				t.Fatalf("count a %s window: the counter refused to count the first event of a quota of three %d times, "+
					"each refusal answering that it was still busy with the key and recording nothing", name, ask)
			}
			select {
			case <-t.Context().Done():
				t.Fatalf("count a %s window: %v", name, t.Context().Err())
			case <-time.After(seedPause):
			}
		}
	}
	count("spent")
	if _, err := admin.ExecContext(t.Context(),
		"UPDATE platformkit_limits SET window_start = now() - interval '2 days'"); err != nil {
		t.Fatalf("age the window: %v", err)
	}
	count("open")

	// What the table holds. The counters are system-only rows, so this reads them as
	// the superuser the purge is not: a case that could not see the table could not
	// say anything about it.
	windows := func() (rows, open int) {
		t.Helper()
		err := admin.QueryRowContext(t.Context(), `SELECT count(*),
			count(*) FILTER (WHERE key LIKE '%open%') FROM platformkit_limits`).Scan(&rows, &open)
		if err != nil {
			t.Fatal(err)
		}
		return rows, open
	}

	// Both windows are there before the job runs, or the count after it would be a
	// statement about a table somebody never filled.
	if before, _ := windows(); before != 2 {
		t.Fatalf("the counter table holds %d rows before the job, want the spent window and the open one", before)
	}
	if err := purge.Run(t.Context(), conn); err != nil {
		t.Fatalf("run %s: %v", purge.Name, err)
	}
	if rows, open := windows(); rows != 1 || open != 1 {
		t.Errorf("after %s the counter table holds %d rows, of which %d are the open window; the promise is that "+
			"a window closed a day ago goes and one still counting stays", purge.Name, rows, open)
	}
}

// seedAsks and seedPause are how long the seeding above will keep asking a busy
// counter. Four asks a quarter-second apart span the wait a loaded machine answers
// in — each ask already waits out the counter's own budget before it refuses — and
// stay far short of becoming this case's watchdog: a counter that is still busy after
// that is the thing the case reports, and it reports it as the counter rather than as
// a row count.
const (
	seedAsks  = 4
	seedPause = 250 * time.Millisecond
)

// kernelJob is the one job of the composition's own list a case is about. Its absence
// is the failure the case reports, and loudly: a job that quietly stopped being
// scheduled is otherwise a table that quietly grows.
func kernelJob(t *testing.T, scheduled []jobs.Job, name string) jobs.Job {
	t.Helper()
	var names []string
	for _, j := range scheduled {
		if j.Name == name {
			return j
		}
		names = append(names, j.Name)
	}
	t.Fatalf("the runner schedules %v and not %s; the composition that writes a table schedules its purge", names, name)
	return jobs.Job{}
}
