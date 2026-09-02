package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
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
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/task"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// The two hosts the tests are served at. Two, because the claim worth proving
// is that one tenant cannot see the other's rows — and now also that one
// tenant's session cannot act at the other's host.
const (
	acmeHost   = "acme.localhost"
	globexHost = "globex.localhost"

	tasksPath  = "/api/v1/task/tasks"
	tenantPath = "/api/v1/tenant/tenants"
	adminEmail = "root@acme.localhost"
	adminPass  = "correct horse battery staple"
)

// quiet keeps a passing test's output to the test's own lines.
func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

// configure writes the configuration the reference app would have read from
// config.yaml, against a schema of this test's own, and returns the path and
// the loaded value. The database is empty: no tables, no ledger, nothing.
func configure(t *testing.T) (string, config.Config) {
	t.Helper()
	migrateURL, appURL := dbtest.URLs(t)
	path := t.TempDir() + "/config.yaml"
	body := "server:\n  addr: \"" + freeAddr(t) + "\"\n  public_host: \"platformkit.localhost\"\n  docs: true\n" +
		"database:\n  url: \"" + appURL + "\"\n  migrate_url: \"" + migrateURL + "\"\n" +
		"nats:\n  url: \"nats://localhost:4222\"\n" +
		"log:\n  level: \"error\"\n" +
		"auth:\n  oidc:\n    issuer: \"\"\n    client_id: \"\"\n    client_secret: \"\"\n    redirect_path: \"\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load the config: %v", err)
	}
	return path, cfg
}

// install runs the bootstrap subcommand exactly as the README's third command
// does: an empty database in, a tenant and an administrator out.
func install(t *testing.T, path string) {
	t.Helper()
	t.Setenv("PLATFORMKIT_BOOTSTRAP_PASSWORD", adminPass)
	err := bootstrap([]string{
		"--config", path, "--tenant", "acme", "--host", acmeHost,
		"--name", "Acme Corporation", "--admin-email", adminEmail,
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
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

// TestAnEmptyDatabaseBecomesAWorkingInstallation is the README's five commands
// as a test, and gate 9: nothing exists, the bootstrap creates the first tenant
// and its administrator, the process migrates and serves, and the whole round
// trip — sign in, create, list, sign out — happens over a cookie.
func TestAnEmptyDatabaseBecomesAWorkingInstallation(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)

	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{
		Tenants: c.tenants, Authorize: c.auth, Authenticate: c.auth.Authenticate,
		Role: app.All, Transport: events.Memory(), Log: quiet(),
	})

	// The probes, at the pod's own address, which names no tenant and carries
	// no cookie: nothing is looked up and no transaction is opened.
	for _, path := range []string{"/health", "/ready"} {
		if code, body := do(t, cfg, nil, http.MethodGet, cfg.Server.Addr, path, ""); code != http.StatusOK {
			t.Errorf("%s = %d %s, want 200", path, code, body)
		}
	}

	// Anonymous, before anything: the task routes are guarded.
	if code, _ := do(t, cfg, nil, http.MethodGet, acmeHost, tasksPath, ""); code != http.StatusForbidden {
		t.Errorf("an anonymous list = %d, want 403", code)
	}

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// A task round trip as the administrator the bootstrap created.
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tasksPath, `{"title":"chiller-2 supply temp","priority":"high"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tasksPath, code, body)
	}
	id := field(t, body, "id")

	if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath, ""); code != http.StatusOK ||
		!strings.Contains(body, `"total":1`) || !strings.Contains(body, "chiller-2") {
		t.Errorf("GET %s = %d %s, want the one task", tasksPath, code, body)
	}

	who := uuid.NewString()
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tasksPath+"/"+id+"/assign", `{"assigneeId":"`+who+`"}`)
	if code != http.StatusOK || !strings.Contains(body, `"`+taskcontracts.StatusAcknowledged+`"`) {
		t.Fatalf("assign = %d %s, want 200 and an acknowledged task", code, body)
	}

	// A second tenant, through the control-plane API, as the administrator of
	// the first: tenant:manage is the permission that reaches every tenant.
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	globexID := uuid.MustParse(field(t, body, "id"))

	// Acme's session, at Globex's host, is nobody. Not because anything
	// compared two tenant ids: the session row is invisible to Globex's
	// transaction, so the lookup finds nothing.
	if code, _ = do(t, cfg, admin, http.MethodGet, globexHost, "/api/v1/auth/me", ""); code != http.StatusForbidden {
		t.Errorf("acme's session at globex = %d, want 403", code)
	}

	// Globex gets its own administrator the way an operator would, and sees an
	// empty task list at its own host.
	provision(t, cfg, globexID, "root@globex.localhost")
	other := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)
	if code, body = do(t, cfg, other, http.MethodGet, globexHost, tasksPath, ""); code != http.StatusOK ||
		!strings.Contains(body, `"total":0`) {
		t.Errorf("GET %s as globex = %d %s, want an empty list", tasksPath, code, body)
	}

	// A host nobody serves is a 404, not a 500 and not somebody's data.
	if code, _ = do(t, cfg, nil, http.MethodGet, "nowhere.localhost", tasksPath, ""); code != http.StatusNotFound {
		t.Errorf("GET %s at an unknown host = %d, want 404", tasksPath, code)
	}

	// Signing out ends it: the same cookie is nobody afterwards.
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/auth/logout", ""); code != http.StatusOK {
		t.Fatalf("logout = %d %s, want 200", code, body)
	}
	if code, _ = do(t, cfg, admin, http.MethodGet, acmeHost, "/api/v1/auth/me", ""); code != http.StatusForbidden {
		t.Errorf("me after logout = %d, want 403", code)
	}
}

// TestBootstrapRefusesAnInstallationThatAlreadyExists: the one write with no
// caller to authorize is safe because it can only ever happen once.
func TestBootstrapRefusesAnInstallationThatAlreadyExists(t *testing.T) {
	path, _ := configure(t)
	install(t, path)
	t.Setenv("PLATFORMKIT_BOOTSTRAP_PASSWORD", adminPass)
	err := bootstrap([]string{
		"--config", path, "--tenant", "globex", "--host", globexHost,
		"--name", "Globex", "--admin-email", "root@globex.localhost",
	})
	if err == nil || !strings.Contains(err.Error(), "already has") {
		t.Errorf("the second bootstrap = %v, want the refusal", err)
	}
}

// TestEveryOperationDeclaresExactlyOneAuthorization is gate 7, read off the
// recording rather than trusted: kit/app runs it at boot, and this says what it
// is checking — every route the whole composition mounts, the new ones
// included, carries one declaration from the closed set of three.
func TestEveryOperationDeclaresExactlyOneAuthorization(t *testing.T) {
	_, cfg := configure(t)
	_, conn := dbtest.Schema(t)
	c := compose(cfg)
	api, _ := httpx.New(httpx.Options{
		PublicHost: cfg.Server.PublicHost, Docs: true, Tenants: c.tenants, Conn: conn,
		Authorize: c.auth, Authenticate: c.auth.Authenticate, Log: quiet(),
	})
	for _, m := range c.modules {
		if m.Routes != nil {
			m.Routes(api)
		}
	}
	health.Register(api, health.DatabaseCheck(conn))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the composition does not declare itself: %v", err)
	}

	kinds := map[string]int{}
	for _, op := range api.Recorded() {
		declared, ok := op.Extensions[httpx.AuthExtension]
		if !ok {
			t.Fatalf("%s %s carries no declaration", op.Method, op.Path)
		}
		encoded, err := json.Marshal(declared)
		if err != nil {
			t.Fatalf("%s %s: %v", op.Method, op.Path, err)
		}
		var read struct {
			Kind       string `json:"kind"`
			Permission string `json:"permission"`
		}
		if err := json.Unmarshal(encoded, &read); err != nil {
			t.Fatalf("%s %s: %v", op.Method, op.Path, err)
		}
		switch read.Kind {
		case "public", "signed_in":
		case "permission":
			if read.Permission == "" {
				t.Errorf("%s %s requires a permission with no name", op.Method, op.Path)
			}
		default:
			t.Errorf("%s %s declares %q, which is not one of the three", op.Method, op.Path, read.Kind)
		}
		kinds[read.Kind]++
	}
	// Every kind is used, which is what makes the closed set worth having.
	for _, kind := range []string{"public", "signed_in", "permission"} {
		if kinds[kind] == 0 {
			t.Errorf("no operation declares %q", kind)
		}
	}
	// And every permission a route asks for is defined by some module, which is
	// the other half of the gate.
	defined := map[string]bool{}
	for _, m := range c.modules {
		for _, p := range m.Permissions {
			defined[p.Key] = true
		}
	}
	for _, p := range api.Permissions() {
		if !defined[p] {
			t.Errorf("permission %q guards a route and is defined by no module", p)
		}
	}
}

// TestTheWorkerRoleSweepsEveryTenant is the other half of docs/adr/0005 and the
// reason the tenant module implements jobs.TenantLister: a process that serves
// no API still migrates, answers the two probes an orchestrator calls, and runs
// the module's periodic work in every tenant there is.
func TestTheWorkerRoleSweepsEveryTenant(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)

	admin := dbtest.Open(t, cfg.Database.MigrateURL)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// A second tenant, and one overdue task in each.
	var tenants []tenancy.Tenant
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if _, err := c.tenants.Create(ctx, tx, tenantcontracts.NewTenant{
			Slug: "globex", Name: "Globex", Host: globexHost,
		}); err != nil {
			return err
		}
		all, err := c.tenants.List(ctx, tx)
		for _, one := range all {
			tenants = append(tenants, one.Tenancy())
		}
		return err
	})
	if err != nil {
		t.Fatalf("create the second tenant: %v", err)
	}
	if len(tenants) != 2 {
		t.Fatalf("there are %d tenants, want two", len(tenants))
	}
	deadline := time.Now().Add(-time.Hour)
	for _, tenant := range tenants {
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return crud.Create(ctx, tx, &taskcontracts.Task{
				Title: "chiller-2 supply temp", Priority: taskcontracts.PriorityCritical, SLADeadline: &deadline,
			})
		})
		if err != nil {
			t.Fatalf("seed %s: %v", tenant.Slug, err)
		}
	}

	// A sweep every 200ms, so two ticks are half a second rather than two
	// minutes. Everything else about the job is what production runs.
	mods := []module.Module{task.Module(task.Deps{
		Tenants: tenantcontracts.Active{Service: c.tenants}, SweepEvery: 200 * time.Millisecond,
	})}
	start(t, cfg, mods, app.Options{
		Tenants: c.tenants, Authorize: c.auth, Authenticate: c.auth.Authenticate,
		Role: app.Worker, Transport: events.Memory(), Log: quiet(),
	})

	if code, body := do(t, cfg, nil, http.MethodGet, cfg.Server.Addr, "/ready", ""); code != http.StatusOK {
		t.Errorf("/ready on a worker = %d %s, want 200", code, body)
	}
	// A worker serves the two probes and nothing else.
	if code, _ := do(t, cfg, nil, http.MethodGet, acmeHost, tasksPath, ""); code != http.StatusNotFound {
		t.Errorf("a worker answered %s with %d; it serves two routes", tasksPath, code)
	}

	until := time.Now().Add(20 * time.Second)
	for {
		var breached int
		if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM tasks WHERE sla_breached`).Scan(&breached); err != nil {
			t.Fatalf("count the breaches: %v", err)
		}
		if breached == 2 {
			return
		}
		if time.Now().After(until) {
			t.Fatalf("the sweep recorded %d breaches across two tenants, want two", breached)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// provision gives a tenant its own administrator, the way the bootstrap gives
// the first one theirs.
func provision(t *testing.T, cfg config.Config, tenantID uuid.UUID, email string) {
	t.Helper()
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	c := compose(cfg)
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		_, err := c.users.Provision(ctx, tx, tenantID, email, "", adminPass, []string{authcontracts.RoleAdmin})
		return err
	})
	if err != nil {
		t.Fatalf("provision %s: %v", email, err)
	}
}

// signIn returns a client holding the session cookie the login set.
func signIn(t *testing.T, cfg config.Config, host, email, password string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}
	code, body := do(t, cfg, client, http.MethodPost, host, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+password+`"}`)
	if code != http.StatusOK {
		t.Fatalf("login as %s = %d %s, want 200", email, code, body)
	}
	return client
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
// which is what decides the tenant. A nil client is an anonymous caller; a
// client with a jar is somebody who has signed in.
func do(t *testing.T, cfg config.Config, client *http.Client, method, host, path, body string) (int, string) {
	t.Helper()
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequest(method, "http://"+cfg.Server.Addr+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
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
