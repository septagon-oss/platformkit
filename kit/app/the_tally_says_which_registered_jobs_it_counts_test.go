package app

// Two cases about the coverage figure this branch publishes.
//
// The brief asks the commit to state `tenant_attributed_boundary_coverage` as
// "registered operations + jobs + subscriptions that emit a tenant-attributed span
// ÷ all", and adds "after the kernel's own". CHANGELOG.md states the reading:
//
//	80 of the 81 boundaries the reference application registers — 69 operations,
//	5 jobs, 7 subscriptions — carry a tenant on their span; the one that does not
//	is `file-reconcile` … The tally counts what the composition registers
//
// The five jobs of that decomposition are the five a *module* declares
// (`grep -rn "jobs.Job{" modules/*/*.go modules/*/internal/*.go`: file-reconcile,
// auth-sweep, billing-renew, task's SLA sweep, audit-retention). This package adds
// four of its own to the scheduler — `work` builds the list as
// `append(kernelJobs(transport, app), a.drainMigrations())` and then appends the
// modules' — and a run of each of those four opens one span whose attributes come
// from a context that holds no tenant, because a purge is the same cross-tenant
// question `file-reconcile` is. So the sentence counts what the composition
// registers and does not count its own four jobs, and the one boundary it names as
// carrying no tenant is not the only one.
//
// The first case below pins the fact (it passes today); the second asks the
// sentence to say what its tally leaves out, and has a passing branch: either name
// the four, or state in words that the tally counts the modules' jobs and not the
// composition's own. It does not pin the size of the tally, so a branch that adds a
// fifth kernel job, or a sixtieth module job, is refused by nothing here once the
// sentence is honest about what it counts.

import (
	"os"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/jobs"
)

// composedKernelJobs is the job list this package puts on the worker, built the way
// `work` builds it, so the case names what the process schedules rather than what a
// reader of this file hopes it schedules.
func composedKernelJobs(t *testing.T) []jobs.Job {
	t.Helper()
	list := append(kernelJobs(memory.New(), appname.Name("")), (&App{}).drainMigrations())
	return list
}

// TestTheCompositionRegistersFourJobsOfItsOwn pins the fact the published tally
// rests on: this package owns four of the scheduled jobs — the relay, the two
// purges and the migration drain — and none of them is one of the module jobs the
// tally's five are. A run span of one of these carries no tenant: the span takes
// `telemetry.SpanAttrs(ctx)` of the scheduler's context (kit/jobs/jobs.go, per run),
// and a tenant span appears only on the walk a per-tenant job makes
// (`<slug> tenant`), which none of these four does.
func TestTheCompositionRegistersFourJobsOfItsOwn(t *testing.T) {
	want := map[string]bool{"outbox-relay": true, "outbox-purge": true, "limit-purge": true, "schema-backfill": true}
	got := map[string]bool{}
	for _, j := range composedKernelJobs(t) {
		got[j.Name] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("the composition does not schedule %q; this case and the published tally both assume it does", name)
		}
	}
	if len(got) != 4 {
		t.Errorf("this package registers %d jobs (%v), and the tally in CHANGELOG.md accounts for none of them by name", len(got), got)
	}
}

// TestThePublishedTallySaysWhichRegisteredJobsItCounts: the release note says the
// tally "counts what the composition registers". Either it names the jobs this
// package registers, or it says in words what its tally leaves out. Naming the
// four, or writing the exclusion, makes this pass; nothing here asks for a
// particular ratio, so the case cannot be satisfied by editing a number and
// nothing in it fails because somebody else's branch added a job.
func TestThePublishedTallySaysWhichRegisteredJobsItCounts(t *testing.T) {
	note, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatalf("read the release note: %v", err)
	}
	paragraph := ""
	for _, p := range strings.Split(string(note), "\n\n") {
		if strings.Contains(p, "boundaries the reference application registers") {
			paragraph = p
			break
		}
	}
	if paragraph == "" {
		t.Fatal("the release note no longer states the tenant-attributed boundary tally this case is about")
	}

	explains := strings.Contains(paragraph, "module") &&
		(strings.Contains(strings.ToLower(paragraph), "not counted") ||
			strings.Contains(strings.ToLower(paragraph), "exclud") ||
			strings.Contains(strings.ToLower(paragraph), "leaves out") ||
			strings.Contains(strings.ToLower(paragraph), "does not count"))
	if explains {
		return // the note says what its tally counts and what it leaves out
	}

	var unnamed []string
	for _, j := range composedKernelJobs(t) {
		if !strings.Contains(paragraph, "`"+j.Name+"`") {
			unnamed = append(unnamed, j.Name)
		}
	}
	if len(unnamed) == 0 {
		return // the note names each job the composition owns
	}
	t.Errorf("the release note says its tally of %s counts what the composition registers, and calls "+
		"`file-reconcile` the one boundary that carries no tenant; the composition registers %v too, "+
		"whose run span carries no tenant either (kit/jobs opens one span per run from a context holding no tenant, "+
		"and the tenant span belongs to a walk these jobs do not make). Name them, or say that the tally counts "+
		"the modules' jobs and not the composition's own.",
		strings.TrimSpace(strings.Split(paragraph, "carry a tenant")[0]), unnamed)
}
