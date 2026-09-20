package jobs

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// spans is the recorder this binary installs as the process's provider, once.
// A Tracer made before the first install stays bound to the provider that first
// install named, and this package made its tracer at initialization, so a test
// that installed a provider of its own per case would read spans that went
// somewhere else. One recorder for the whole binary, cleared per test, is what
// every instrumented package reports into.
var spans = tracetest.NewSpanRecorder()

func TestMain(m *testing.M) {
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	os.Exit(m.Run())
}

// fresh empties the recorder, so every span a test below finds is one that test
// made. Nothing in this package runs in parallel, so nothing else is writing.
func fresh(t *testing.T) {
	t.Helper()
	spans.Reset()
}

// ended finds the one span a test just made. Jobs run on the caller's goroutine,
// so unlike a delivery there is nothing to wait for: a missing span is a missing
// span.
func ended(t *testing.T, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	var saw []string
	for _, span := range spans.Ended() {
		if span.Name() == name {
			return span
		}
		saw = append(saw, span.Name())
	}
	t.Fatalf("no span named %q; saw %v", name, saw)
	return nil
}

func wantAttribute(t *testing.T, span sdktrace.ReadOnlySpan, key, want string) {
	t.Helper()
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			if kv.Value.Emit() != want {
				t.Errorf("%s %s = %s, want %q", span.Name(), key, kv.Value.Emit(), want)
			}
			return
		}
	}
	t.Errorf("%s has no %s attribute", span.Name(), key)
}

// TestEveryOutcomeOfARunIsOneSpan: what a scheduled job reports is not only that
// it failed, but which of the three ways it did not run this was. The third is
// the one a log line at Debug hides — a cluster whose replicas all believe another
// one is running the job is indistinguishable from one that is running it, from
// outside, and this is the view from inside.
func TestEveryOutcomeOfARunIsOneSpan(t *testing.T) {
	_, conn := dbtest.Schema(t)
	fresh(t)
	boom := errors.New("the report could not be written")
	ran := false
	s := NewScheduler(conn, nil,
		Job{Name: "tidy", Every: time.Minute, Parallel: true,
			Run: func(context.Context, *db.Conn) error { return nil }},
		Job{Name: "report", Every: time.Minute, Parallel: true,
			Run: func(context.Context, *db.Conn) error { return boom }},
		Job{Name: "settle", Every: time.Minute,
			Run: func(context.Context, *db.Conn) error { ran = true; return nil }},
	)
	s.run(t.Context(), s.jobs[0].job)
	s.run(t.Context(), s.jobs[1].job)

	unlock, held, err := db.TryLock(t.Context(), conn, "job:settle")
	if err != nil || !held {
		t.Fatalf("holding the lock: held=%v err=%v", held, err)
	}
	s.run(t.Context(), s.jobs[2].job)
	unlock()
	if ran {
		t.Fatal("the job ran while another replica held its lock")
	}

	tidy := ended(t, "tidy run")
	wantAttribute(t, tidy, "platformkit.job.parallel", "true")
	wantAttribute(t, tidy, "platformkit.job.outcome", "ok")
	if tidy.Status().Code == codes.Error {
		t.Errorf("a job that succeeded is marked %v", tidy.Status())
	}

	report := ended(t, "report run")
	wantAttribute(t, report, "platformkit.job.outcome", "error")
	if status := report.Status(); status.Code != codes.Error || status.Description != boom.Error() {
		t.Errorf("the failed job is %v, want an error status saying %q", status, boom)
	}

	settle := ended(t, "settle run")
	wantAttribute(t, settle, "platformkit.job.parallel", "false")
	wantAttribute(t, settle, "platformkit.job.outcome", "another replica")
	if settle.Status().Code == codes.Error {
		t.Error("a run that lost the lock is not a failure, and must not read as one")
	}
}

// TestPerTenantSpansOneTenantEach is the answer to "which tenant took the eight
// seconds": a job that walks four hundred tenants is one span whose duration is a
// sum nobody can attribute, unless each tenant is a span of its own. One failing
// tenant does not stop the others and does not mark them either.
func TestPerTenantSpansOneTenantEach(t *testing.T) {
	_, conn := dbtest.Schema(t)
	fresh(t)
	bad := errors.New("globex has a row this job cannot read")
	tenants := lister{
		{ID: uuid.New(), Slug: "acme"},
		{ID: uuid.New(), Slug: "globex"},
	}
	if err := PerTenant(t.Context(), conn, tenants, func(_ context.Context, _ *db.Conn, tenant tenancy.Tenant) error {
		if tenant.Slug == "globex" {
			return bad
		}
		return nil
	}); err == nil {
		t.Fatal("PerTenant reported no failure for a tenant whose work failed")
	}

	// Both names on a span from a job that walks tenants, because such a job is
	// given the slug with the row: the slug is what a reader recognises and the
	// id is what matches this span to the same tenant's delivery spans.
	wantAttribute(t, ended(t, "acme tenant"), "platformkit.tenant", "acme")
	wantAttribute(t, ended(t, "acme tenant"), "platformkit.tenant.id", tenants[0].ID.String())
	globex := ended(t, "globex tenant")
	wantAttribute(t, globex, "platformkit.tenant", "globex")
	wantAttribute(t, globex, "platformkit.tenant.id", tenants[1].ID.String())
	if status := globex.Status(); status.Code != codes.Error || status.Description != bad.Error() {
		t.Errorf("the failed tenant is %v, want an error status saying %q", status, bad)
	}
}

// TestPerTenantConcurrentSpansEveryTenant is the same claim through the pooled
// path, where the callbacks are on other goroutines and the spans must still be
// children of the job's rather than of whichever worker happened to take the
// index.
func TestPerTenantConcurrentSpansEveryTenant(t *testing.T) {
	_, conn := dbtest.Schema(t)
	fresh(t)
	want := []string{"one", "two", "three", "four"}
	tenants := make(lister, len(want))
	ids := map[string]string{}
	for i, slug := range want {
		tenants[i] = tenancy.Tenant{ID: uuid.New(), Slug: slug}
		ids[slug] = tenants[i].ID.String()
	}
	if err := PerTenantConcurrent(t.Context(), conn, tenants, 3, func(context.Context, *db.Conn, tenancy.Tenant) error {
		return nil
	}); err != nil {
		t.Fatalf("PerTenantConcurrent: %v", err)
	}
	for _, slug := range want {
		span := ended(t, slug+" tenant")
		wantAttribute(t, span, "platformkit.tenant", slug)
		wantAttribute(t, span, "platformkit.tenant.id", ids[slug])
	}
}
