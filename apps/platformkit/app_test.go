package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

// quiet keeps a passing test's output to the test's own lines.
func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

// The two hosts the tests are served at. Two, because the claim worth proving
// is that one tenant cannot see the other's rows.
const (
	acmeHost   = "acme.localhost"
	globexHost = "globex.localhost"
)

const tasksPath = "/api/v1/task/tasks"

// compose builds the configuration the reference app would have read from
// config.yaml, against a schema of this test's own. The database is empty: no
// tables, no ledger, nothing. That is the point of the first test.
func compose(t *testing.T) (config.Config, *dev) {
	t.Helper()
	migrateURL, appURL := dbtest.URLs(t)
	cfg := config.Config{
		Server:   config.Server{Addr: freeAddr(t), PublicHost: "platformkit.localhost", Docs: true},
		Database: config.Database{URL: appURL, MigrateURL: migrateURL},
		NATS:     config.NATS{URL: "nats://localhost:4222"},
		Log:      config.Log{Level: "error"},
		Dev: config.Dev{
			Enabled:   true,
			Principal: config.DevPrincipal{UserID: uuid.NewString(), Roles: []string{"admin"}},
			Tenants: []config.DevTenant{
				{Host: acmeHost, ID: uuid.NewString(), Slug: "acme", Name: "Acme"},
				{Host: globexHost, ID: uuid.NewString(), Slug: "globex", Name: "Globex"},
			},
		},
	}
	d, err := newDev(cfg)
	if err != nil {
		t.Fatalf("newDev: %v", err)
	}
	return cfg, d
}

// start runs the application in the background and returns when it is listening.
func start(t *testing.T, cfg config.Config, mods []module.Module, opts app.Options) {
	t.Helper()
	a, err := app.New(t.Context(), cfg, mods, opts)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan error, 1)
	go func() { stopped <- a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-stopped; err != nil {
			t.Errorf("Run: %v", err)
		}
	})
	waitFor(t, cfg.Server.Addr)
}

// TestTheApplicationBootsFromNothing is the empty-database boot, gate 9. The
// schema holds no tables at all; the process migrates it, resolves a tenant
// from a request host, serves the task module's routes, relays what they
// published, and refuses the two things it must refuse.
func TestTheApplicationBootsFromNothing(t *testing.T) {
	cfg, d := compose(t)

	// A test module, so that the outbox relay has somewhere to deliver to. It
	// subscribes to an event the task module declares, which is the only kind
	// of subscription kit/app lets a composition contain.
	assigned := make(chan events.Event, 4)
	probe := module.Module{
		Name: "probe",
		Subscriptions: []events.Subscription{{
			Module: "probe", Name: contracts.EventAssigned,
			Handler: func(_ context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
				if db.TenantOf(tx).ID != ev.TenantID {
					t.Errorf("the handler ran in tenant %s for an event of %s", db.TenantOf(tx).ID, ev.TenantID)
				}
				assigned <- ev
				return nil
			},
		}},
	}

	start(t, cfg, append(modules(d), probe), app.Options{
		Tenants: d, Authorize: d, Authenticate: d.Authenticate,
		Role: app.All, Transport: events.Memory(), Log: quiet(),
	})

	// The probes, at the pod's own address, which names no tenant.
	for _, path := range []string{"/health", "/ready"} {
		if code, body := do(t, cfg, http.MethodGet, cfg.Server.Addr, path, ""); code != http.StatusOK {
			t.Errorf("%s = %d %s, want 200", path, code, body)
		}
	}

	// A task round trip as the development principal.
	code, body := do(t, cfg, http.MethodPost, acmeHost, tasksPath, `{"title":"chiller-2 supply temp","priority":"high"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tasksPath, code, body)
	}
	id := field(t, body, "id")

	if code, body = do(t, cfg, http.MethodGet, acmeHost, tasksPath, ""); code != http.StatusOK ||
		!strings.Contains(body, `"total":1`) || !strings.Contains(body, "chiller-2") {
		t.Errorf("GET %s = %d %s, want the one task", tasksPath, code, body)
	}

	who := uuid.NewString()
	code, body = do(t, cfg, http.MethodPost, acmeHost, tasksPath+"/"+id+"/assign", `{"assigneeId":"`+who+`"}`)
	if code != http.StatusOK || !strings.Contains(body, `"`+contracts.StatusAcknowledged+`"`) {
		t.Fatalf("assign = %d %s, want 200 and an acknowledged task", code, body)
	}

	// The outbox relay is running in this process, so what the request wrote
	// reaches the subscription without anybody publishing to a broker.
	select {
	case ev := <-assigned:
		if ev.TenantID != d.byHost[acmeHost].ID || !strings.Contains(string(ev.Payload), who) {
			t.Errorf("the subscription saw %+v", ev)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the relay never delivered task.assigned")
	}

	// Row-level security, from outside: the second tenant's host is served by
	// the same process, the same connection and the same handler, and sees none
	// of the first tenant's rows.
	if code, body = do(t, cfg, http.MethodGet, globexHost, tasksPath, ""); code != http.StatusOK ||
		!strings.Contains(body, `"total":0`) {
		t.Errorf("GET %s as globex = %d %s, want an empty list", tasksPath, code, body)
	}

	// And a host nobody serves is a 404, not a 500 and not somebody's data.
	if code, _ = do(t, cfg, http.MethodGet, "nowhere.localhost", tasksPath, ""); code != http.StatusNotFound {
		t.Errorf("GET %s at an unknown host = %d, want 404", tasksPath, code)
	}
}

// TestTheWorkerRoleSweepsTheSLA is the other half of docs/adr/0005: a process
// that serves no API still migrates, answers the two probes an orchestrator
// calls, and runs the module's periodic work.
func TestTheWorkerRoleSweepsTheSLA(t *testing.T) {
	cfg, d := compose(t)
	// The worker migrates too, but this test writes its task first, so the
	// schema has to exist before Run does anything. Applying it twice is the
	// ordinary case; see docs/adr/0005.
	if err := db.Migrate(t.Context(), cfg.Database.MigrateURL, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	admin := dbtest.Open(t, cfg.Database.MigrateURL)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	acme := d.byHost[acmeHost]

	overdue := &contracts.Task{Title: "chiller-2 supply temp", Priority: contracts.PriorityCritical}
	deadline := time.Now().Add(-time.Hour)
	overdue.SLADeadline = &deadline
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return crud.Create(ctx, tx, overdue)
	})
	if err != nil {
		t.Fatalf("seed an overdue task: %v", err)
	}

	// A sweep every 200ms, so two ticks are half a second rather than two
	// minutes. Everything else about the job is what production runs.
	mods := []module.Module{task.Module(task.Deps{Tenants: d, SweepEvery: 200 * time.Millisecond})}
	start(t, cfg, mods, app.Options{
		Tenants: d, Authorize: d, Authenticate: d.Authenticate,
		Role: app.Worker, Transport: events.Memory(), Log: quiet(),
	})

	if code, body := do(t, cfg, http.MethodGet, cfg.Server.Addr, "/ready", ""); code != http.StatusOK {
		t.Errorf("/ready on a worker = %d %s, want 200", code, body)
	}
	// A worker serves the two probes and nothing else.
	if code, _ := do(t, cfg, http.MethodGet, acmeHost, tasksPath, ""); code != http.StatusNotFound {
		t.Errorf("a worker answered %s with %d; it serves two routes", tasksPath, code)
	}

	deadline2 := time.Now().Add(10 * time.Second)
	for {
		var breached bool
		if err := admin.QueryRowContext(t.Context(),
			`SELECT sla_breached FROM tasks WHERE id = $1`, overdue.ID).Scan(&breached); err != nil {
			t.Fatalf("read the task: %v", err)
		}
		if breached {
			return
		}
		if time.Now().After(deadline2) {
			t.Fatal("the sweep never recorded the breach")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestTheDevelopmentIdentityIsRefusedOffALaptop: everything in dev.go is an
// administrator of every tenant, so the one thing that must not be possible is
// turning it on for a host the internet can reach.
func TestTheDevelopmentIdentityIsRefusedOffALaptop(t *testing.T) {
	dir := t.TempDir() + "/config.yaml"
	write := func(publicHost string) {
		t.Helper()
		body := `server:
  addr: ":8080"
  public_host: "` + publicHost + `"
database:
  url: "postgres://a@localhost/x"
  migrate_url: "postgres://b@localhost/x"
nats:
  url: "nats://localhost:4222"
log:
  level: "info"
dev:
  enabled: true
  principal:
    user_id: "00000000-0000-0000-0000-0000000000a1"
  tenants:
    - host: "x.localhost"
      id: "00000000-0000-0000-0000-0000000000b1"
      slug: "x"
      name: "X"
`
		if err := os.WriteFile(dir, []byte(body), 0o600); err != nil {
			t.Fatalf("write the config: %v", err)
		}
	}

	write("tasks.example.com")
	if _, err := config.Load(dir); err == nil || !strings.Contains(err.Error(), "local name") {
		t.Errorf("Load = %v, want the refusal", err)
	}
	write("platformkit.localhost:8080")
	if _, err := config.Load(dir); err != nil {
		t.Errorf("Load of a local host = %v", err)
	}
}

func waitFor(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("nothing is listening on %s", addr)
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

// do sends one request to the running application at the given Host header,
// which is what decides the tenant.
func do(t *testing.T, cfg config.Config, method, host, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, "http://"+cfg.Server.Addr+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s at %s: %v", method, path, host, err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(out)
}

func field(t *testing.T, body, name string) string {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("read %s from %s: %v", name, body, err)
	}
	s, _ := out[name].(string)
	if s == "" {
		t.Fatalf("no %s in %s", name, body)
	}
	return s
}
