package health_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const tenantHost = "acme.test"

// sites serves one tenant and knows that nothing else exists, which is the
// situation both an orchestrator's probe and a real request arrive in.
type sites struct{ tenant tenancy.Tenant }

func (s sites) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != tenantHost {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return s.tenant, nil
}

func (sites) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) { return false, nil }

// anonymous is the identity hook for a file about probes: the two probes are
// Public and carry no credential, so the kernel never asks it anything. It is
// here because httpx.New requires one — an API that could not recognise a
// caller could only fail closed on every request.
func anonymous(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
	return tenancy.Principal{}, false, nil
}

func serve(t *testing.T, checks ...health.Check) (http.Handler, *db.Conn) {
	t.Helper()
	_, app := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		Tenants:      sites{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme"}},
		Conn:         app,
		Authorize:    sites{},
		Authenticate: anonymous,
	})
	health.Register(api, checks...)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the probes are not declared: %v", err)
	}
	return router, app
}

// counting is a loader that records every question it is asked and takes its
// time answering, which is what a loader whose database is unreachable does:
// the query runs to the kernel's two second budget and only then fails.
type counting struct {
	sites
	delay time.Duration
	calls atomic.Int64
}

func (c *counting) ByHost(ctx context.Context, tx db.Tx[db.System], h string) (tenancy.Tenant, error) {
	c.calls.Add(1)
	select {
	case <-time.After(c.delay):
	case <-ctx.Done():
	}
	return c.sites.ByHost(ctx, tx, h)
}

func probe(t *testing.T, h http.Handler, host, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://"+host+path, nil))
	return w
}

// check is a Check a test can make fail. The package used to export a Func
// adapter for this and nothing outside these tests ever used it: there is one
// Check in the application, and a test that needs a second one declares it.
type check struct {
	name string
	err  error
}

func (c check) Name() string                { return c.name }
func (c check) Check(context.Context) error { return c.err }

// TestLivenessIgnoresTheChecks: a probe that fails while the database blinks
// gets the process killed instead of getting the database fixed.
func TestLivenessIgnoresTheChecks(t *testing.T) {
	h, _ := serve(t, check{name: "always-broken", err: errors.New("down")})
	res := probe(t, h, "10.0.0.7:8080", "/health")
	if res.Code != http.StatusOK {
		t.Fatalf("/health = %d, want 200", res.Code)
	}
	if !strings.Contains(res.Body.String(), `"status":"ok"`) {
		t.Errorf("/health body = %s", res.Body.String())
	}
}

// TestReadinessNamesTheFailingChecks and nothing else about them.
func TestReadinessNamesTheFailingChecks(t *testing.T) {
	h, _ := serve(t,
		check{name: "queue"},
		check{name: "search-index", err: errors.New("dial tcp 10.0.0.1:9200: connection refused")},
	)
	res := probe(t, h, "10.0.0.7:8080", "/ready")
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("/ready = %d, want 503", res.Code)
	}
	body := res.Body.String()
	if !strings.Contains(body, "search-index") {
		t.Errorf("/ready does not name the failing check: %s", body)
	}
	for _, leak := range []string{"queue", "10.0.0.1", "connection refused"} {
		if strings.Contains(body, leak) {
			t.Errorf("/ready leaked %q: %s", leak, body)
		}
	}
}

// TestTheProbesAnswerWithTheDatabaseDown, at the pod's address and at a tenant
// host alike. This is the whole reason the request transaction is opened on
// first use: liveness that fails during a database outage restarts every
// replica, and readiness that answers 500 instead of 503 tells the orchestrator
// the instance is broken rather than not ready yet.
func TestTheProbesAnswerWithTheDatabaseDown(t *testing.T) {
	h, app := serve(t)
	// One request while the database still answers, so the host is resolved and
	// the outage below is about the probes rather than about the lookup.
	probe(t, h, tenantHost, "/health")

	if err := app.Close(); err != nil {
		t.Fatalf("close the pool: %v", err)
	}

	for _, host := range []string{"10.0.0.7:8080", tenantHost} {
		if got := probe(t, h, host, "/health").Code; got != http.StatusOK {
			t.Errorf("/health at %s = %d with the database down, want 200", host, got)
		}
	}
}

// TestReadinessIs503WhenTheDatabaseIsDown, at either kind of host, and names
// the check that failed.
func TestReadinessIs503WhenTheDatabaseIsDown(t *testing.T) {
	_, app := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		Tenants:      sites{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme"}},
		Conn:         app,
		Authorize:    sites{},
		Authenticate: anonymous,
	})
	health.Register(api, health.DatabaseCheck(app))

	if got := probe(t, router, tenantHost, "/ready").Code; got != http.StatusOK {
		t.Fatalf("/ready with a live database = %d, want 200", got)
	}
	if err := app.Close(); err != nil {
		t.Fatalf("close the pool: %v", err)
	}
	for _, host := range []string{"10.0.0.7:8080", tenantHost} {
		res := probe(t, router, host, "/ready")
		if res.Code != http.StatusServiceUnavailable {
			t.Errorf("/ready at %s = %d with the database down, want 503", host, res.Code)
		}
		if !strings.Contains(res.Body.String(), "database") {
			t.Errorf("/ready at %s does not name the check: %s", host, res.Body.String())
		}
	}
}

// TestDatabaseCheckRunsOutsideAnyTenantTransaction. Readiness is not tenant
// work, so it opens a system transaction; kit/db refuses one nested inside an
// open tenant transaction, and a probe request never opens one, which is why
// /ready above works at a tenant host.
func TestDatabaseCheckRunsOutsideAnyTenantTransaction(t *testing.T) {
	_, app := dbtest.Schema(t)
	check := health.DatabaseCheck(app)
	if check.Name() != "database" {
		t.Errorf("Name = %q, want %q", check.Name(), "database")
	}
	if err := check.Check(t.Context()); err != nil {
		t.Fatalf("database check: %v", err)
	}

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
		return check.Check(ctx)
	})
	if !errors.Is(err, db.ErrScopeMismatch) {
		t.Fatalf("a check run inside an open tenant transaction = %v, want ErrScopeMismatch", err)
	}
}

// TestTheProbesNeverResolveTheHost is the property the deployment rests on.
//
// Liveness used to go through the kernel's host resolution: a real query, with
// a two second budget of its own, and never a cache hit at a pod address —
// only a successful resolution is cached. So a probe with a two second timeout
// failed while the database was unreachable, and three periods later the
// kubelet restarted a pod whose only problem was that its database was down.
//
// The loader here answers, eventually. What is under test is that nobody asks
// it: both probes answer immediately, and the counter is zero.
func TestTheProbesNeverResolveTheHost(t *testing.T) {
	_, app := dbtest.Schema(t)
	slow := &counting{
		sites: sites{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme"}},
		delay: 3 * time.Second,
	}
	api, router := httpx.New(httpx.Options{
		Tenants: slow, Conn: app, Authorize: sites{}, Authenticate: anonymous,
	})
	health.Register(api, check{name: "queue"})

	// Both hosts: the pod address an orchestrator uses, and a tenant's own
	// name, which is what a probe through an ingress arrives as.
	for _, host := range []string{"10.0.0.7:8080", tenantHost, "nobody.example"} {
		for _, path := range []string{"/health", "/ready"} {
			started := time.Now()
			res := probe(t, router, host, path)
			if res.Code != http.StatusOK {
				t.Errorf("%s at %s = %d, want 200", path, host, res.Code)
			}
			if took := time.Since(started); took > time.Second {
				t.Errorf("%s at %s took %v; a probe that waits on the host lookup is a probe that fails during an outage", path, host, took)
			}
		}
	}
	if n := slow.calls.Load(); n != 0 {
		t.Errorf("the probes asked the loader %d times; they resolve no tenant", n)
	}
}

// TestReadinessAnswersWithinTheProbeTimeout, with the database down. Two
// seconds is what the deployment gives a probe; a readiness check that took
// longer would read as a timeout rather than as 503, and the difference is
// whether the orchestrator holds traffic off this instance or restarts it.
func TestReadinessAnswersWithinTheProbeTimeout(t *testing.T) {
	_, app := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		Tenants:      sites{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme"}},
		Conn:         app,
		Authorize:    sites{},
		Authenticate: anonymous,
	})
	health.Register(api, health.DatabaseCheck(app))
	if err := app.Close(); err != nil {
		t.Fatalf("close the pool: %v", err)
	}

	started := time.Now()
	if got := probe(t, router, "10.0.0.7:8080", "/health").Code; got != http.StatusOK {
		t.Errorf("/health with the database down = %d, want 200", got)
	}
	res := probe(t, router, "10.0.0.7:8080", "/ready")
	if res.Code != http.StatusServiceUnavailable {
		t.Errorf("/ready with the database down = %d, want 503", res.Code)
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Errorf("the two probes took %v; the deployment's timeout is 2s", took)
	}
	if !strings.Contains(res.Body.String(), "database") {
		t.Errorf("/ready does not name the check: %s", res.Body.String())
	}
}
