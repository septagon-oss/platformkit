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
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
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
	auditPath  = "/api/v1/audit/events"
	noticePath = "/api/v1/notification/notifications"
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

	// The worker half of this process is running, so what the routes published
	// is on its way. Audit subscribes to every event every other module
	// declares — main computes that list with module.EventNames — so the task
	// the administrator just created is in the trail, with the administrator as
	// its actor. Nothing registered that: the task module emitted an event and
	// the kernel put the caller on the envelope.
	me := field(t, whoami(t, cfg, admin), "userId")
	created := waitForAudit(t, cfg, admin, taskcontracts.EventCreated)
	if created["actor"] != me {
		t.Errorf("the trail credits the task to %v, want the administrator %s", created["actor"], me)
	}
	if got := waitForAudit(t, cfg, admin, taskcontracts.EventAssigned)["actor"]; got != me {
		t.Errorf("the trail credits the assignment to %v, want %s", got, me)
	}

	// A notification, raised the way another module will raise one: through the
	// service main holds, inside a tenant transaction. It asks for mail, so the
	// worker renders it and hands it to the mailbox this composition wired
	// because config.Mail names no server.
	notice := notify(t, cfg, c, uuid.MustParse(me))
	if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, noticePath, ""); code != http.StatusOK ||
		!strings.Contains(body, `"total":1`) || !strings.Contains(body, notice.String()) {
		t.Fatalf("GET %s = %d %s, want the administrator's own notice", noticePath, code, body)
	}
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, noticePath+"/"+notice.String()+"/read", ""); code != http.StatusOK {
		t.Fatalf("marking it read = %d %s, want 200", code, body)
	}
	// And the trail records that too, which is the loop closing: a module the
	// audit module has never heard of publishes, and the row appears.
	waitForAudit(t, cfg, admin, notificationcontracts.EventRead)

	box, ok := c.mail.(*notificationtest.Mailbox)
	if !ok {
		t.Fatalf("the composition wired %T as its mailer, want the mailbox", c.mail)
	}
	eventually(t, "the notice reaches the mailbox", func() bool { return len(box.Sent()) == 1 })
	if sent := box.Sent()[0]; sent.To != adminEmail || !strings.Contains(sent.Body, "chiller-2") {
		t.Errorf("the mailbox holds %+v, want the notice addressed to the administrator", sent)
	}

	// A second tenant, through the control-plane API, as the administrator of
	// the first. Acme is the operator's own tenant — the bootstrap created it —
	// so its admin role names tenant:manage and the kernel lets the request
	// through. That is the only tenant in the installation where this works.
	//
	// A body that asks to be an operator is refused outright, and that is the
	// schema rather than a check somebody wrote: NewTenant.Operator is
	// json:"-", so the field is in no request body and huma refuses the
	// property it does not know.
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"evil","name":"Evil","host":"evil.localhost","operator":true}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "operator") {
		t.Fatalf("POST %s with an operator flag = %d %s, want 422 naming it", tenantPath, code, body)
	}

	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	if strings.Contains(body, `"operator":true`) {
		t.Fatalf("POST %s made an operator tenant: %s", tenantPath, body)
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

	// The probe E3.1's review ran, inverted. It signed in as the second
	// tenant's administrator and listed, created and suspended tenants — with
	// the wildcard that tenant's admin role holds by construction, at that
	// tenant's own host, because the control plane is served at every host and
	// tenant:manage was an ordinary permission a wildcard satisfied.
	//
	// Globex's administrator holds the same wildcard now. Every one of these is
	// a 403 before the roles table is consulted at all: the permission is
	// declared Operator, and Globex is not the operator's tenant.
	for _, probe := range []struct{ method, path, body string }{
		{http.MethodGet, tenantPath, ""},
		{http.MethodPost, tenantPath, `{"slug":"evil","name":"Evil","host":"evil.localhost"}`},
		{http.MethodPost, tenantPath + "/" + globexID.String() + "/suspend", ""},
		{http.MethodGet, tenantPath + "/" + globexID.String(), ""},
		{http.MethodPost, tenantPath + "/" + globexID.String() + "/hosts", `{"host":"evil.localhost"}`},
	} {
		code, body = do(t, cfg, other, probe.method, globexHost, probe.path, probe.body)
		if code != http.StatusForbidden {
			t.Errorf("%s %s as globex's admin = %d %s, want 403", probe.method, probe.path, code, body)
		}
		if !strings.Contains(body, "AUTH_NOT_OPERATOR") {
			t.Errorf("%s %s was refused for the wrong reason: %s", probe.method, probe.path, body)
		}
	}
	// And a role in a non-operator tenant that names the permission outright is
	// still refused: the kernel never asks.
	grant(t, cfg, globexID, "root@globex.localhost")
	other = signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)
	if code, body = do(t, cfg, other, http.MethodGet, globexHost, tenantPath, ""); code != http.StatusForbidden {
		t.Errorf("a globex role naming tenant:manage = %d %s, want 403", code, body)
	}

	// A host nobody serves is a 404, not a 500 and not somebody's data.
	if code, _ = do(t, cfg, nil, http.MethodGet, "nowhere.localhost", tasksPath, ""); code != http.StatusNotFound {
		t.Errorf("GET %s at an unknown host = %d, want 404", tasksPath, code)
	}

	// Inviting somebody is the loop this stage closes: the user module creates
	// a person with no password and publishes user.invited, the auth module
	// subscribes to that, issues a one-time token and asks the notification
	// module to mail the link, and the worker sends it. Nothing in the user
	// module knows any of that happens.
	invite(t, cfg, c, "grace@acme.localhost")
	var link string
	eventually(t, "the invitation to be mailed", func() bool {
		for _, sent := range box.Sent() {
			if sent.To == "grace@acme.localhost" {
				link = sent.Body
				return true
			}
		}
		return false
	})
	token := tokenIn(t, link)

	// The link works, once, and it is what turns an invitation into somebody
	// who can sign in.
	if code, body = do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/auth/password/reset",
		`{"token":"`+token+`","new":"a chosen passphrase for grace"}`); code != http.StatusOK {
		t.Fatalf("the reset = %d %s, want 200", code, body)
	}
	signIn(t, cfg, acmeHost, "grace@acme.localhost", "a chosen passphrase for grace")
	if code, body = do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/auth/password/reset",
		`{"token":"`+token+`","new":"another passphrase entirely"}`); code != http.StatusUnauthorized {
		t.Errorf("the link worked twice = %d %s, want 401", code, body)
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
		case "permission", "operator_permission":
			if read.Permission == "" {
				t.Errorf("%s %s requires a permission with no name", op.Method, op.Path)
			}
		default:
			t.Errorf("%s %s declares %q, which is not one of the four", op.Method, op.Path, read.Kind)
		}
		kinds[read.Kind]++
	}
	// Every kind is used, which is what makes the closed set worth having.
	for _, kind := range []string{"public", "signed_in", "permission", "operator_permission"} {
		if kinds[kind] == 0 {
			t.Errorf("no operation declares %q", kind)
		}
	}
	// And every permission a route asks for is defined by some module, with the
	// same kind on both sides. The kind is the half that matters most: a
	// control-plane route declared as an ordinary permission is a route every
	// customer's administrator reaches through the wildcard they hold in their
	// own tenant, which is exactly the hole E3.1's review found.
	operatorOf := map[string]bool{}
	defined := map[string]bool{}
	for _, m := range c.modules {
		for _, p := range m.Permissions {
			defined[p.Key], operatorOf[p.Key] = true, p.Operator
		}
	}
	operators := 0
	for _, g := range api.Required() {
		switch {
		case !defined[g.Permission]:
			t.Errorf("permission %q guards a route and is defined by no module", g.Permission)
		case operatorOf[g.Permission] != g.Operator:
			t.Errorf("permission %q is operator=%v on its route and operator=%v in its manifest",
				g.Permission, g.Operator, operatorOf[g.Permission])
		}
		if g.Operator {
			operators++
		}
	}
	// One route kind is only worth having if something declares it.
	if operators == 0 {
		t.Error("no route declares an operator permission; the control plane is guarded by an ordinary one")
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

// whoami is the caller's own identity, which is where the test learns the
// administrator's user id: the same id the kernel stamps on every event their
// requests publish.
func whoami(t *testing.T, cfg config.Config, client *http.Client) string {
	t.Helper()
	code, body := do(t, cfg, client, http.MethodGet, acmeHost, "/api/v1/auth/me", "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/v1/auth/me = %d %s, want 200", code, body)
	}
	return body
}

// waitForAudit is the first trail row with this event name, once the worker has
// got to it. The relay runs once a second, so this is a wait and not a read:
// what it proves is that the row arrives, not how soon.
func waitForAudit(t *testing.T, cfg config.Config, client *http.Client, name string) map[string]any {
	t.Helper()
	var row map[string]any
	eventually(t, "the trail to record "+name, func() bool {
		code, body := do(t, cfg, client, http.MethodGet, acmeHost, auditPath+"?name="+name, "")
		if code != http.StatusOK {
			t.Fatalf("GET %s = %d %s, want 200", auditPath, code, body)
		}
		var out struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("read the trail from %s: %v", body, err)
		}
		if len(out.Items) == 0 {
			return false
		}
		row = out.Items[0]
		return true
	})
	return row
}

// notify raises one notification the way another module will: through the
// service main holds, inside the tenant's own transaction. It asks for mail, so
// the worker has something to send.
func notify(t *testing.T, cfg config.Config, c composition, recipient uuid.UUID) uuid.UUID {
	t.Helper()
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()

	var acme tenancy.Tenant
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		acme, err = c.tenants.ByHost(ctx, tx, acmeHost)
		return err
	})
	if err != nil {
		t.Fatalf("resolve %s: %v", acmeHost, err)
	}

	var id uuid.UUID
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := c.notify.Notify(ctx, tx, notificationcontracts.Notice{
			Recipient: recipient, Title: "chiller-2 supply temp is out of band",
			Body: "The task is waiting for somebody.", Link: "/admin/task/tasks", Email: true,
		})
		if err == nil {
			id = row.ID
		}
		return err
	})
	if err != nil {
		t.Fatalf("notify the administrator: %v", err)
	}
	return id
}

// eventually waits for something the worker does. Everything it is used for is
// asynchronous by design — the relay ticks once a second — so a test that read
// once would be testing the tick and not the behaviour.
func eventually(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
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

// grant writes a role in a tenant that names the control plane's permission
// outright, and puts the user in it: the strongest thing a customer with
// database access to their own rows could do for themselves.
//
// It is the case the operator flag exists for. The wildcard not satisfying an
// operator grant is one refusal; this is the other, and it is the kernel's — no
// role in this tenant is ever asked about, because the tenant is not the
// operator's.
func grant(t *testing.T, cfg config.Config, tenantID uuid.UUID, email string) {
	t.Helper()
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	err = dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		if err := tx.DB().Exec(
			`INSERT INTO roles (tenant_id, name, permissions) VALUES (?, 'operator', ARRAY['tenant:manage'])`,
			tenantID).Error; err != nil {
			return err
		}
		return tx.DB().Exec(
			`UPDATE users SET roles = ARRAY['admin','operator'] WHERE tenant_id = ? AND email = ?`,
			tenantID, email).Error
	})
	if err != nil {
		t.Fatalf("grant tenant:manage in %s: %v", tenantID, err)
	}
}

// invite creates somebody with no password, through the service main holds.
//
// It is the service and not a route because inviting is user.Service.Invite,
// and the module mounts no route for it: the generic create publishes
// user.user.created and this flow is driven by user.invited. That the two are
// different events is the user module's business; what this test is about is
// what the auth module does when it sees the second one.
func invite(t *testing.T, cfg config.Config, c composition, email string) {
	t.Helper()
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	var acme tenancy.Tenant
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		acme, err = c.tenants.ByHost(ctx, tx, acmeHost)
		return err
	})
	if err != nil {
		t.Fatalf("resolve %s: %v", acmeHost, err)
	}
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.users.Invite(ctx, tx, email, "")
		return err
	})
	if err != nil {
		t.Fatalf("invite %s: %v", email, err)
	}
}

// tokenIn is the set-password token a mailed link carries. The message is the
// notification module's plain-text template with the auth module's link in it,
// so this is the one place a test reads across the two.
func tokenIn(t *testing.T, body string) string {
	t.Helper()
	_, after, ok := strings.Cut(body, "token=")
	if !ok {
		t.Fatalf("no token in the mail:\n%s", body)
	}
	token, _, _ := strings.Cut(after, "\n")
	return strings.TrimSpace(token)
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
