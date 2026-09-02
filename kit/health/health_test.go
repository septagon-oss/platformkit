package health_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
func anonymous(context.Context, db.Tx[db.Tenant], *http.Request) (httpx.Principal, bool, error) {
	return httpx.Principal{}, false, nil
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

func probe(t *testing.T, h http.Handler, host, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://"+host+path, nil))
	return w
}

// TestLivenessIgnoresTheChecks: a probe that fails while the database blinks
// gets the process killed instead of getting the database fixed.
func TestLivenessIgnoresTheChecks(t *testing.T) {
	h, _ := serve(t, health.Func{N: "always-broken", F: func(context.Context) error {
		return errors.New("down")
	}})
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
		health.Func{N: "queue", F: func(context.Context) error { return nil }},
		health.Func{N: "search-index", F: func(context.Context) error {
			return errors.New("dial tcp 10.0.0.1:9200: connection refused")
		}},
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
