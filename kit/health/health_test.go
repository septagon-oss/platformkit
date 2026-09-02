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
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// probes is the smallest thing httpx.New will accept: no tenant anywhere, which
// is the situation an orchestrator's probe actually arrives in.
type probes struct{}

func (probes) ByHost(context.Context, string) (tenancy.Tenant, error) {
	return tenancy.Tenant{}, errors.New("no tenant")
}
func (probes) Allowed(context.Context, tenancy.Tenant, string) (bool, error) { return false, nil }

func serve(t *testing.T, checks ...health.Check) http.Handler {
	t.Helper()
	_, app := db.TestSchema(t)
	api, router := httpx.New(httpx.Options{
		Tenants:      probes{},
		Conn:         app,
		Authorize:    probes{},
		Authenticate: func(*http.Request) (httpx.Principal, bool) { return httpx.Principal{}, false },
	})
	health.Register(api, checks...)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the probes are not declared: %v", err)
	}
	return router
}

func probe(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://10.0.0.7:8080"+path, nil))
	return w
}

// TestLivenessIgnoresTheChecks: a probe that fails while the database blinks
// gets the process killed instead of getting the database fixed.
func TestLivenessIgnoresTheChecks(t *testing.T) {
	h := serve(t, health.Func{N: "always-broken", F: func(context.Context) error {
		return errors.New("down")
	}})
	res := probe(t, h, "/health")
	if res.Code != http.StatusOK {
		t.Fatalf("/health = %d, want 200", res.Code)
	}
	if !strings.Contains(res.Body.String(), `"status":"ok"`) {
		t.Errorf("/health body = %s", res.Body.String())
	}
}

// TestReadinessNamesTheFailingChecks and nothing else about them.
func TestReadinessNamesTheFailingChecks(t *testing.T) {
	h := serve(t,
		health.Func{N: "queue", F: func(context.Context) error { return nil }},
		health.Func{N: "search-index", F: func(context.Context) error {
			return errors.New("dial tcp 10.0.0.1:9200: connection refused")
		}},
	)
	res := probe(t, h, "/ready")
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

// TestDatabaseCheckPassesAndRunsOutsideAnyRequestTransaction. The second half is
// the part that matters: readiness is not tenant work, so it must survive being
// called from inside a request that has a tenant transaction open, which kit/db
// would otherwise refuse as a scope mismatch.
func TestDatabaseCheckPassesAndRunsOutsideAnyRequestTransaction(t *testing.T) {
	_, app := db.TestSchema(t)
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
		t.Fatalf("a check run on a request context = %v, want ErrScopeMismatch; /ready strips the request's values for exactly this reason", err)
	}
}
