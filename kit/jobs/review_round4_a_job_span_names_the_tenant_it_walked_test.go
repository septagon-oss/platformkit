package jobs

// Review round 4 (T-0110). The brief's denominator counts jobs among the boundaries that
// must "emit a tenant-attributed span", and the CHANGELOG says 80 of the 81 registered
// boundaries carry a tenant on their span. No case in this package had ever read a span
// back: nothing in kit/jobs' own tests names OpenTelemetry, so the claim about the job
// boundary rested on one line — the context the attributes are read from.
//
// 	span := telemetry.Tracer().Start(ctx, tenant.Slug+" tenant",
// 		trace.WithAttributes(telemetry.SpanAttrs(tenancy.WithTenant(ctx, tenant))...))
//
// That call reads its attributes from a context built for the purpose, not from the `ctx`
// the worker was handed, which carries no tenant at all. Swap the two and every span this
// path opens — for every tenant, on every replica — carries no tenant: the job boundary
// drops out of the number the brief asked this branch to state, and the reader of a slow
// job span still cannot tell whose tenants were slow. Nothing would have noticed. This
// case reads the spans back: one per tenant, each naming its own tenant by both keys, and
// never the other tenant's.

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestEachTenantsShareOfAJobNamesOnlyItsOwnTenant: two tenants, one walk, and the spans
// read back off an in-memory exporter.
func TestEachTenantsShareOfAJobNamesOnlyItsOwnTenant(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))

	_, conn := dbtest.Schema(t)
	acme, globex := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}, tenancy.Tenant{ID: uuid.New(), Slug: "globex"}

	err := PerTenantConcurrent(t.Context(), conn, lister{acme, globex}, 2,
		func(ctx context.Context, _ *db.Conn, tenant tenancy.Tenant) error {
			held, ok := tenancy.FromContext(ctx)
			if !ok || held.ID != tenant.ID {
				t.Errorf("the callback for %s was handed a context carrying %v, want that same tenant",
					tenant.Slug, held.ID)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("walk the two tenants: %v", err)
	}

	want := map[string]tenancy.Tenant{acme.Slug + " tenant": acme, globex.Slug + " tenant": globex}
	seen := map[string]bool{}
	for _, s := range recorder.Ended() {
		tenant, named := want[s.Name()]
		if !named {
			continue
		}
		seen[s.Name()] = true
		attrs := s.Attributes()
		id, hasID := attr(attrs, telemetry.AttrTenantID)
		slug, hasSlug := attr(attrs, telemetry.AttrTenant)
		if !hasID || id != tenant.ID.String() {
			t.Errorf("the span %q carries pkit.tenant.id=%q, want %s: a job span that does not name the "+
				"tenant whose rows it touched is not a tenant-attributed boundary, and the job walk is the "+
				"boundary five of the reference application's jobs are (%v)",
				s.Name(), id, tenant.ID, attrs)
			continue
		}
		if !hasSlug || slug != tenant.Slug {
			t.Errorf("the span %q carries pkit.tenant=%q, want %q — this path holds both names, so a span "+
				"without the slug drops the one name an operator reads (%v)", s.Name(), slug, tenant.Slug, attrs)
		}
		for _, other := range []tenancy.Tenant{acme, globex} {
			if other.ID == tenant.ID {
				continue
			}
			if v, ok := attr(attrs, telemetry.AttrTenantID); ok && v == other.ID.String() {
				t.Errorf("the span %q names tenant %s, which is not the one it walked", s.Name(), other.ID)
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("no span named %q was recorded; a walk of two tenants opens one span per tenant", name)
		}
	}
}

// TestAJobRunSpanSaysHowItEnded pins the other half of the boundary the branch added: the
// span the scheduler opens per run, and the one attribute that says what happened. A run
// refused by another replica, a run whose job failed and a run that finished are three
// different facts to an operator, and "the span exists" says none of them.
func TestAJobRunSpanSaysHowItEnded(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))

	_, conn := dbtest.Schema(t)
	succeeded := Job{Name: "round4_ok", Every: time.Minute, Parallel: true,
		Run: func(context.Context, *db.Conn) error { return nil }}
	failed := Job{Name: "round4_failed", Every: time.Minute, Parallel: true,
		Run: func(context.Context, *db.Conn) error { return errors.New("the round-4 job failed") }}

	s := NewScheduler(conn, slog.New(slog.NewTextHandler(discard{}, nil)), appname.Name(""), succeeded, failed)
	s.run(t.Context(), succeeded)
	s.run(t.Context(), failed)

	ended := recorder.Ended()
	got := map[string]string{}
	for _, sp := range ended {
		if outcome, ok := attr(sp.Attributes(), "pkit.job.outcome"); ok {
			got[sp.Name()] = outcome
		}
	}
	if got[succeeded.Name+" run"] != "ok" {
		t.Errorf("the span of a run that succeeded says %q, want \"ok\": %v", got[succeeded.Name+" run"], got)
	}
	if failure, has := got[failed.Name+" run"]; !has || failure != "error" {
		t.Errorf("the span of a run whose job failed says %q, want \"error\": %v", failure, got)
	}
}

func attr(attrs []attribute.KeyValue, key string) (string, bool) {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value.AsString(), true
		}
	}
	return "", false
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
