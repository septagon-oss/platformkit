package app

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const tenantHost = "acme.test"

// fixture is the three answers main has to supply: which host is which tenant,
// who is calling, and what they may do.
type fixture struct{ tenant tenancy.Tenant }

func (f fixture) ByHost(_ context.Context, h string) (tenancy.Tenant, error) {
	if h != tenantHost {
		return tenancy.Tenant{}, errors.New("no tenant at " + h)
	}
	return f.tenant, nil
}

func (fixture) Allowed(context.Context, tenancy.Tenant, string) (bool, error) { return true, nil }

func compose(t *testing.T) (config.Config, Options) {
	t.Helper()
	migrateURL, appURL := db.TestSchemaURLs(t)
	cfg := config.Config{
		Server:   config.Server{Addr: freeAddr(t), PublicHost: tenantHost},
		Database: config.Database{URL: appURL, MigrateURL: migrateURL},
		NATS:     config.NATS{URL: "nats://localhost:4222"},
		Log:      config.Log{Level: "error"},
	}
	opts := Options{
		Tenants:      fixture{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}},
		Authorize:    fixture{},
		Authenticate: func(*http.Request) (httpx.Principal, bool) { return httpx.Principal{}, false },
		Log:          slog.New(slog.DiscardHandler),
	}
	return cfg, opts
}

// freeAddr picks a port the kernel has just confirmed is free.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

type helloOut struct {
	Body struct {
		Tenant string `json:"tenant"`
	}
}

// hello is a module that carries its own SQL and one public route which writes
// through the transaction the kernel opened for it.
func hello() module.Module {
	return module.Module{
		Name:        "hello",
		Permissions: []module.Permission{{Key: "note:write", Description: "Write notes"}},
		Events:      []string{"hello.note_written"},
		Nav:         []module.NavEntry{{Label: "Notes", Path: "/hello", Permission: "note:write", Order: 1}},
		Migrations: fstest.MapFS{
			"000002_notes.up.sql":   {Data: []byte(`CREATE TABLE notes (id serial PRIMARY KEY, tenant_id uuid NOT NULL)`)},
			"000002_notes.down.sql": {Data: []byte(`DROP TABLE notes`)},
		},
		Routes: func(api *httpx.API) {
			httpx.Register(api, huma.Operation{
				OperationID: "hello", Method: http.MethodGet, Path: "/hello",
			}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*helloOut, error) {
				tx, ok := httpx.TxFrom(ctx)
				if !ok {
					return nil, errors.New("the handler has no transaction")
				}
				tenant := db.TenantOf(tx)
				if err := tx.DB().Exec("INSERT INTO notes (tenant_id) VALUES (?)", tenant.ID.String()).Error; err != nil {
					return nil, err
				}
				out := &helloOut{}
				out.Body.Tenant = tenant.Slug
				return out, nil
			})
			httpx.Register(api, huma.Operation{
				OperationID: "whoami", Method: http.MethodGet, Path: "/me",
			}, httpx.SignedIn(), func(context.Context, *struct{}) (*helloOut, error) {
				return &helloOut{}, nil
			})
		},
	}
}

// TestBootMigratesAndServes is the empty-database boot: nothing exists, Run
// applies migrations/ and the module's SQL through the one ledger, resolves the
// tenant from the request host, and answers both probes and the module's route.
func TestBootMigratesAndServes(t *testing.T) {
	cfg, opts := compose(t)
	a, err := New(t.Context(), cfg, []module.Module{hello()}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan error, 1)
	go func() { stopped <- a.Run(ctx) }()
	waitFor(t, cfg.Server.Addr)

	// The probes as an orchestrator sends them: at the pod's address, which
	// names no tenant, so no transaction is opened at all.
	for _, path := range []string{"/health", "/ready"} {
		if code, body := get(t, cfg.Server.Addr, cfg.Server.Addr, path); code != http.StatusOK {
			t.Errorf("%s at the pod address = %d %s, want 200", path, code, body)
		}
	}

	// And readiness at a tenant host, where a transaction is open around the
	// handler: the database check has to run outside it.
	if code, body := get(t, cfg.Server.Addr, tenantHost, "/ready"); code != http.StatusOK {
		t.Errorf("/ready at %s = %d %s, want 200", tenantHost, code, body)
	}

	// The module's route, its table, and its tenant.
	code, body := get(t, cfg.Server.Addr, tenantHost, "/hello")
	if code != http.StatusOK {
		t.Fatalf("/hello = %d %s, want 200", code, body)
	}
	if !strings.Contains(body, `"tenant":"acme"`) {
		t.Errorf("/hello = %s, want the resolved tenant", body)
	}

	// A host nobody serves is a 404 for anything that is not public, and the
	// anonymous caller this test configures reaches nothing that is not.
	if code, _ := get(t, cfg.Server.Addr, "elsewhere.test", "/me"); code != http.StatusNotFound {
		t.Errorf("/me at an unknown host = %d, want 404", code)
	}
	if code, _ := get(t, cfg.Server.Addr, tenantHost, "/me"); code != http.StatusForbidden {
		t.Errorf("/me as an anonymous caller = %d, want 403", code)
	}

	cancel()
	if err := <-stopped; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// TestBootRefusesAnUndeclaredOperation is gate 7. The operation is hidden, so
// it is in no OpenAPI document; only the adapter recording sees it.
func TestBootRefusesAnUndeclaredOperation(t *testing.T) {
	cfg, opts := compose(t)
	backdoor := module.Module{Name: "backdoor", Routes: func(api *httpx.API) {
		huma.Register(api, huma.Operation{
			OperationID: "backdoor", Method: http.MethodGet, Path: "/backdoor", Hidden: true,
		}, func(context.Context, *struct{}) (*helloOut, error) { return &helloOut{}, nil })
	}}
	a, err := New(t.Context(), cfg, []module.Module{backdoor}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = a.Run(t.Context())
	if err == nil {
		t.Fatal("Run served an application with an undeclared operation")
	}
	if !strings.Contains(err.Error(), "GET /backdoor (backdoor)") {
		t.Errorf("the error does not name the operation: %v", err)
	}
	// Run returned while its context was still alive, so it never reached the
	// listener; nothing is serving on the port it would have taken.
	if c, dialErr := net.DialTimeout("tcp", cfg.Server.Addr, time.Second); dialErr == nil {
		_ = c.Close()
		t.Error("the application listened before validating its declarations")
	}
}

// TestNewRefusesAnInvalidComposition: the module rules are checked before
// anything is opened.
func TestNewRefusesAnInvalidComposition(t *testing.T) {
	cfg, opts := compose(t)
	_, err := New(t.Context(), cfg, []module.Module{{Name: "a"}, {Name: "a"}}, opts)
	if err == nil || !strings.Contains(err.Error(), "declared twice") {
		t.Fatalf("New = %v, want the duplicate name", err)
	}
	if _, err := New(t.Context(), cfg, nil, Options{}); err == nil {
		t.Fatal("New accepted Options with nothing in them")
	}
}

// TestMigrationVersionsAreGlobal: one ledger means one numbering. A module that
// reuses a version is refused by name rather than silently never applied.
func TestMigrationVersionsAreGlobal(t *testing.T) {
	clash := module.Module{Name: "clash", Migrations: fstest.MapFS{
		"000001_other.up.sql": {Data: []byte("SELECT 1")},
	}}
	_, err := sources([]module.Module{clash})
	if err == nil {
		t.Fatal("sources accepted a module that reuses version 000001")
	}
	for _, want := range []string{"000001", "migrations/", "module clash"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %q: %v", want, err)
		}
	}

	// A module that embedded its migrations directory rather than the files in
	// it contributes nothing, which has to be an error and not a quiet boot.
	_, err = sources([]module.Module{{Name: "nested", Migrations: fstest.MapFS{
		"migrations/000009_x.up.sql": {Data: []byte("SELECT 1")},
	}}})
	if err == nil || !strings.Contains(err.Error(), "fs.Sub") {
		t.Errorf("sources = %v, want the fs.Sub hint", err)
	}

	// A module numbered past the root's migrations joins the same directory.
	merged, err := sources([]module.Module{hello()})
	if err != nil {
		t.Fatalf("sources: %v", err)
	}
	entries, err := fs.ReadDir(merged, ".")
	if err != nil {
		t.Fatalf("read the merged directory: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	for _, want := range []string{"000001_tenancy.up.sql", "000002_notes.up.sql"} {
		if !strings.Contains(strings.Join(names, " "), want) {
			t.Errorf("the merged directory lacks %s: %v", want, names)
		}
	}
}

func waitFor(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("nothing is listening on %s", addr)
}

func get(t *testing.T, addr, host, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s at %s: %v", path, host, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body)
}
