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
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
)

const tenantHost = "acme.test"

// fixture is the three answers main has to supply: which host is which tenant,
// who is calling, and what they may do.
type fixture struct{ tenant tenancy.Tenant }

func (f fixture) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != tenantHost {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return f.tenant, nil
}

func (fixture) Allowed(context.Context, tenancy.Tenant, string) (bool, error) { return true, nil }

func compose(t *testing.T) (config.Config, Options) {
	t.Helper()
	migrateURL, appURL := dbtest.URLs(t)
	cfg := config.Config{
		Server:   config.Server{Addr: freeAddr(t), PublicHost: tenantHost, Docs: true},
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
			"000003_notes.up.sql":   {Data: []byte(`CREATE TABLE notes (id serial PRIMARY KEY, tenant_id uuid NOT NULL)`)},
			"000003_notes.down.sql": {Data: []byte(`DROP TABLE notes`)},
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

// TestBootRefusesARouteNoModuleCanReach is the declaration gate's mirror: a
// permission no module defines is a route nobody can ever be granted. The other
// half of gate 7 — an operation with no declaration at all — cannot be written
// from out here any more, and lives in kit/httpx's internal test.
func TestBootRefusesARouteNoModuleCanReach(t *testing.T) {
	cfg, opts := compose(t)
	ghost := module.Module{Name: "ghost", Routes: func(api *httpx.API) {
		httpx.Register(api, huma.Operation{
			OperationID: "haunt", Method: http.MethodGet, Path: "/haunt",
		}, httpx.Permission("ghost:read"), func(context.Context, *struct{}) (*helloOut, error) {
			return &helloOut{}, nil
		})
	}}
	a, err := New(t.Context(), cfg, []module.Module{ghost}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = a.Run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "ghost:read") {
		t.Fatalf("Run = %v, want the undefined permission", err)
	}
	// Run returned while its context was still alive, so it never reached the
	// listener; nothing is serving on the port it would have taken.
	if c, dialErr := net.DialTimeout("tcp", cfg.Server.Addr, time.Second); dialErr == nil {
		_ = c.Close()
		t.Error("the application listened before validating its composition")
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
	for _, want := range []string{"000001_tenancy.up.sql", "000003_notes.up.sql"} {
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

// Widget is an entity mounted through kit/crud, for the boot gate below.
type Widget struct {
	crud.Base
	Name string `json:"name"`
}

func (Widget) TableName() string { return "widgets" }

// TestBootRefusesAnEventNoModulePromised is the third gate. A crud.Spec
// publishes three events; a module that mounts one and declares none is a
// module whose events no subscriber could ever be written against, because the
// manifest is where a subscriber looks.
func TestBootRefusesAnEventNoModulePromised(t *testing.T) {
	cfg, opts := compose(t)
	shop := func(events []string) module.Module {
		return module.Module{
			Name:        "shop",
			Permissions: []module.Permission{{Key: "widget:read"}, {Key: "widget:write"}},
			Events:      events,
			Routes: func(api *httpx.API) {
				crud.Spec[*Widget]{
					Module: "shop", Entity: "widget", Path: "/widgets",
					Read: "widget:read", Write: "widget:write",
				}.Mount(api)
			},
		}
	}

	a, err := New(t.Context(), cfg, []module.Module{shop(nil)}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = a.Run(t.Context())
	if err == nil {
		t.Fatal("Run served routes that publish events no module declared")
	}
	for _, want := range []string{"shop.widget.created", "shop.widget.updated", "shop.widget.deleted"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %s: %v", want, err)
		}
	}

	// Declaring them is all it takes, and then the composition boots.
	cfg, opts = compose(t)
	declared := shop([]string{"shop.widget.created", "shop.widget.updated", "shop.widget.deleted"})
	a, err = New(t.Context(), cfg, []module.Module{declared}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan error, 1)
	go func() { stopped <- a.Run(ctx) }()
	waitFor(t, cfg.Server.Addr)
	cancel()
	if err := <-stopped; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// TestWorkerRelaysAndAnswersItsProbes is the worker role end to end: it
// migrates, answers the two probes an orchestrator calls, relays the outbox and
// runs the module's subscription — for a row this test wrote through a
// connection of its own, which is what "another process" looks like from here.
func TestWorkerRelaysAndAnswersItsProbes(t *testing.T) {
	migrateURL, appURL := dbtest.URLs(t)
	// The worker migrates too, but the test writes its event first, so the
	// schema has to exist before Run does anything. Applying it twice is the
	// ordinary case: see docs/adr/0005.
	if err := db.Migrate(t.Context(), migrateURL, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	conn, err := db.Open(t.Context(), appURL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	cfg := config.Config{
		Server:   config.Server{Addr: freeAddr(t), PublicHost: tenantHost},
		Database: config.Database{URL: appURL, MigrateURL: migrateURL},
		NATS:     config.NATS{URL: "nats://localhost:4222"},
		Log:      config.Log{Level: "error"},
	}
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	handled := make(chan events.Event, 4)
	ledger := module.Module{
		Name:   "ledger",
		Events: []string{"ledger.entry_written"},
		Subscriptions: []events.Subscription{{
			Module: "ledger", Name: "ledger.entry_written",
			Handler: func(_ context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
				if db.TenantOf(tx).ID != ev.TenantID {
					return errors.New("the handler ran in the wrong tenant")
				}
				handled <- ev
				return nil
			},
		}},
	}
	opts := Options{
		Tenants:      fixture{tenant: tenant},
		Authorize:    fixture{},
		Authenticate: func(*http.Request) (httpx.Principal, bool) { return httpx.Principal{}, false },
		Log:          slog.New(slog.DiscardHandler),
		Role:         Worker,
		// The transport a single worker uses to talk to itself. JetStream is
		// the default for this role and kit/events tests it; what is under test
		// here is that the worker relays at all.
		Transport: events.Memory(),
	}

	a, err := New(t.Context(), cfg, []module.Module{ledger}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan error, 1)
	go func() { stopped <- a.Run(ctx) }()
	waitFor(t, cfg.Server.Addr)

	// A worker serves the two probes and nothing else.
	for _, path := range []string{"/health", "/ready"} {
		if code, body := get(t, cfg.Server.Addr, cfg.Server.Addr, path); code != http.StatusOK {
			t.Errorf("%s = %d %s, want 200", path, code, body)
		}
	}
	if code, _ := get(t, cfg.Server.Addr, cfg.Server.Addr, "/openapi.json"); code != http.StatusNotFound {
		t.Errorf("a worker answered /openapi.json with %d; it serves two routes", code)
	}

	writeErr := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(tx, "ledger.entry_written", map[string]any{"amount": 7})
	})
	if writeErr != nil {
		t.Fatalf("publish: %v", writeErr)
	}

	select {
	case ev := <-handled:
		if ev.TenantID != tenant.ID || !strings.Contains(string(ev.Payload), "7") {
			t.Errorf("the subscription saw %+v", ev)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the worker never relayed the row")
	}

	cancel()
	if err := <-stopped; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// TestRoleIsAClosedSet: a role nobody implements would be a process that
// silently does half the work, so it is refused where it is written.
func TestRoleIsAClosedSet(t *testing.T) {
	cfg, opts := compose(t)
	opts.Role = "migrate"
	if _, err := New(t.Context(), cfg, nil, opts); err == nil || !strings.Contains(err.Error(), "migrate") {
		t.Errorf("New = %v, want the unknown role", err)
	}
	opts.Role = ""
	a, err := New(t.Context(), cfg, nil, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.opts.Role != All {
		t.Errorf("the default role is %q, want %q", a.opts.Role, All)
	}
}
