package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/task"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// The two hosts the tests are served at. Two, because the claim worth proving
// is that one tenant cannot see the other's rows — and now also that one
// tenant's session cannot act at the other's host.
const (
	acmeHost   = "acme.localhost"
	globexHost = "globex.localhost"
	// initechHost is the third, and it exists for one claim: a tenant the
	// control plane created gets a working administrator through the control
	// plane, with no SQL and no shell. Acme's is the bootstrap's and Globex's
	// is provisioned in-process, so neither of them could say it.
	initechHost = "initech.localhost"

	// The tenant API and the plan catalog are the installation's own: they are
	// mounted on the control plane, which is served at the installation host and
	// answered with a 404 everywhere else. Their read doors — the plans a customer
	// may see — stay in that customer's workspace.
	tenantPath = "/api/v1/ops/tenant/tenants"
	plansWrite = "/api/v1/ops/billing/plans"

	tasksPath   = "/api/v1/task/tasks"
	usersPath   = "/api/v1/user/users"
	invitePath  = "/api/v1/user/invitations"
	auditPath   = "/api/v1/audit/events"
	noticePath  = "/api/v1/notification/notifications"
	plansPath   = "/api/v1/billing/plans"
	subPath     = "/api/v1/billing/subscription"
	contentPath = "/api/v1/content/contents"
	sitePath    = "/api/v1/site/settings"
	filesPath   = "/api/v1/file/files"
	adminEmail  = "root@acme.localhost"
	adminPass   = "correct horse battery staple"
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
	// The file module keeps its bytes under a directory of this test's own, so
	// a suite that uploads something leaves nothing behind and two suites
	// running at once do not share a disk.
	// The installation is reached at acme's host, which is the operator's own
	// tenant (the bootstrap created it): the control plane is served where the
	// installation is, and nowhere else. TestTheControlPlaneIsNotFoundAtATenantHost
	// in kit/httpx is where globex's answer at the same address is proved.
	body := "server:\n  addr: \"" + freeAddr(t) + "\"\n  public_host: \"platformkit.localhost\"\n  installation_host: \"" + acmeHost + "\"\n  docs: true\n" +
		"database:\n  url: \"" + appURL + "\"\n  migrate_url: \"" + migrateURL + "\"\n" +
		"nats:\n  url: \"nats://localhost:4222\"\n" +
		"log:\n  level: \"error\"\n" +
		"files:\n  dir: \"" + t.TempDir() + "\"\n" +
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
//
// A case that does not name a catalog renderer gets the product's own: the
// composition gate refuses a composition that registers resources and renders no
// workspace document, and a test that skipped it would be starting something the
// product would never boot.
func start(t *testing.T, cfg config.Config, mods []module.Module, opts app.Options) {
	t.Helper()
	if opts.WorkspaceCatalog == nil {
		opts.WorkspaceCatalog = func(ctx context.Context, resources []httpx.Resource) (any, error) {
			return screens.Describe(ctx, resources), nil
		}
	}
	// Same for the installation's host: the control plane is served where the
	// installation is reached, and a test that left it out would be starting a
	// process that answers its own operator at no address at all.
	if opts.Installation.Host == "" {
		opts.Installation = app.Installation{Host: cfg.Server.InstallationHost}
	}
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
		Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate,
		Role: app.All, Transport: memory.New(), Log: quiet(),
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
	// The trail is a plan feature in this product — modules.go prices it, the
	// audit module only declares that it has one — so a tenant that has bought
	// nothing is told to buy something rather than told it may not look. 402
	// and not 403: "ask your administrator" and "upgrade" are different
	// sentences and only one of them is true here.
	if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, auditPath, ""); code != http.StatusPaymentRequired ||
		!strings.Contains(body, "PLAN_EXCLUDES") {
		t.Fatalf("the trail with no subscription = %d %s, want 402", code, body)
	}

	// Billing: a plan, and the tenant on it. The first period is a trial —
	// this application serves it before it asks for anything — and the
	// subscription is a singleton, so there is one to read and no list. The
	// plan includes the trail, which is what makes the reads below possible.
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, plansWrite,
		`{"code":"pro","name":"Pro","priceCents":2900,"currency":"EUR","interval":"month","active":true,"features":["audit-trail"]}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", plansWrite, code, body)
	}
	planID := field(t, body, "id")
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, subPath+"/subscribe", `{"planId":"`+planID+`"}`); code != http.StatusOK ||
		!strings.Contains(body, `"status":"trial"`) {
		t.Fatalf("subscribe = %d %s, want 200 and a trial", code, body)
	}
	if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, subPath, ""); code != http.StatusOK ||
		!strings.Contains(body, planID) {
		t.Errorf("GET %s = %d %s, want the subscription", subPath, code, body)
	}

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

	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the composition wired %T as its mailer, want the mailbox", c.mail)
	}
	eventually(t, "the notice reaches the mailbox", func() bool { return len(box.Sent()) == 1 })
	if sent := box.Sent()[0]; sent.To != adminEmail || !strings.Contains(sent.Body, "chiller-2") {
		t.Errorf("the mailbox holds %+v, want the notice addressed to the administrator", sent)
	}

	// The four modules a product is made of, one round trip each, as the
	// administrator the bootstrap created. Billing went first, above, because
	// the trail this test already read is something its plan includes.
	//
	// Content: written, published, and then read at the same host by a caller
	// with no session at all — which is what publishing means. The script in
	// the body is not in the page: the renderer leaves raw HTML out and the
	// sanitizer refuses what is left.
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, contentPath,
		`{"slug":"About Us","title":"About Acme","kind":"page","body":"# About\n\n<script>alert(1)</script>\n\nWe make **things**."}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", contentPath, code, body)
	}
	pageID := field(t, body, "id")
	if code, body = do(t, cfg, nil, http.MethodGet, acmeHost, "/api/v1/content/public/about-us", ""); code != http.StatusNotFound {
		t.Errorf("an unpublished page = %d %s, want 404", code, body)
	}
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, contentPath+"/"+pageID+"/publish", ""); code != http.StatusOK {
		t.Fatalf("publish = %d %s, want 200", code, body)
	}
	code, body = do(t, cfg, nil, http.MethodGet, acmeHost, "/api/v1/content/public/about-us", "")
	if code != http.StatusOK || !strings.Contains(body, "<strong>things</strong>") {
		t.Fatalf("the published page = %d %s", code, body)
	}
	if strings.Contains(body, "<script") || strings.Contains(body, "alert(1)") {
		t.Errorf("the published page carries the script somebody typed into it:\n%s", body)
	}

	// Site: what a theme reads, saved by an administrator and read by nobody
	// in particular. The tagline and the home slug are not in the public
	// answer, because a public response that carried the whole row would be an
	// admin screen anybody could read.
	if code, body = do(t, cfg, admin, http.MethodPut, acmeHost, sitePath,
		`{"title":"Acme","tagline":"We make things","homeSlug":"about-us","theme":"dark","nav":[{"label":"About","path":"/about-us"}]}`); code != http.StatusOK {
		t.Fatalf("PUT %s = %d %s, want 200", sitePath, code, body)
	}
	code, body = do(t, cfg, nil, http.MethodGet, acmeHost, "/api/v1/site/settings/public", "")
	if code != http.StatusOK || !strings.Contains(body, `"theme":"dark"`) || !strings.Contains(body, `"path":"/about-us"`) {
		t.Fatalf("the public site = %d %s", code, body)
	}
	if strings.Contains(body, "tagline") || strings.Contains(body, "homeSlug") {
		t.Errorf("the public site carries what only an administrator asked for:\n%s", body)
	}

	// File: a kilobyte up, the same kilobyte back, and then gone — the row now
	// and the bytes once the worker has handled file.deleted, which is the one
	// thing in this architecture that happens exactly after a commit.
	const kilobyte = 1024
	code, body = putFile(t, cfg, admin, filesPath, "notes.txt", "text/plain", strings.Repeat("x", kilobyte))
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", filesPath, code, body)
	}
	fileID := field(t, body, "id")
	if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, filesPath+"/"+fileID+"/content", ""); code != http.StatusOK ||
		len(body) != kilobyte {
		t.Fatalf("the download = %d, %d bytes, want the kilobyte back", code, len(body))
	}
	if got := blobs(t, cfg); got != 1 {
		t.Fatalf("the storage holds %d blobs, want the one that was uploaded", got)
	}
	if code, body = do(t, cfg, admin, http.MethodDelete, acmeHost, filesPath+"/"+fileID, ""); code != http.StatusNoContent {
		t.Fatalf("DELETE %s = %d %s, want 204", filesPath, code, body)
	}
	if code, _ = do(t, cfg, admin, http.MethodGet, acmeHost, filesPath+"/"+fileID+"/content", ""); code != http.StatusNotFound {
		t.Errorf("the deleted file's content = %d, want 404", code)
	}
	eventually(t, "the worker to remove the bytes behind a deleted file", func() bool { return blobs(t, cfg) == 0 })

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
	// Globex's administrator holds the same wildcard now, and it is worth them
	// nothing here: these routes are mounted on the control plane, and the control
	// plane is served at the installation's host and at no other. Globex's host
	// therefore answers each one exactly as it answers an address nobody mounted —
	// 404, no allow-list, nothing about the surface disclosed. It is refused before
	// the host is even resolved to a tenant, and so before the roles table exists
	// to be asked.
	//
	// The second, independent guarantee — that a tenant which is not the
	// installation's cannot exercise an operator permission even at the
	// installation host, however its roles are written — is
	// TestTheControlPlaneIsNotFoundAtATenantHost in kit/httpx, which holds the
	// installation host fixed and changes only the tenant.
	for _, probe := range []struct{ method, path, body string }{
		{http.MethodGet, tenantPath, ""},
		{http.MethodPost, tenantPath, `{"slug":"evil","name":"Evil","host":"evil.localhost"}`},
		{http.MethodPost, tenantPath + "/" + globexID.String() + "/suspend", ""},
		{http.MethodGet, tenantPath + "/" + globexID.String(), ""},
		{http.MethodPost, tenantPath + "/" + globexID.String() + "/hosts", `{"host":"evil.localhost"}`},
	} {
		code, body = do(t, cfg, other, probe.method, globexHost, probe.path, probe.body)
		if code != http.StatusNotFound {
			t.Errorf("%s %s as globex's admin = %d %s, want 404: no control plane is served at this host",
				probe.method, probe.path, code, body)
		}
	}
	// And a role in a non-operator tenant that names the permission outright is
	// still refused. The answer is the 404 of an address this host does not serve,
	// which is the same thing a caller with no grant at all is told: what the
	// control plane is not is nobody's business, and a 403 here would confirm that
	// the surface exists and that the difference is somebody's role.
	grant(t, cfg, globexID, "root@globex.localhost")
	other = signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)
	if code, body = do(t, cfg, other, http.MethodGet, globexHost, tenantPath, ""); code != http.StatusNotFound {
		t.Errorf("a globex role naming tenant:manage = %d %s, want 404: no control plane is served at this host", code, body)
	}

	// A host nobody serves is a 404, not a 500 and not somebody's data.
	if code, _ = do(t, cfg, nil, http.MethodGet, "nowhere.localhost", tasksPath, ""); code != http.StatusNotFound {
		t.Errorf("GET %s at an unknown host = %d, want 404", tasksPath, code)
	}

	// A tenant the control plane created, given its first administrator by the
	// control plane. It is the hole E3.2's review found: every route that makes
	// a user is a tenant route, authorized inside that tenant's own
	// transaction, and the operator is at their own host — so a tenant created
	// here had nobody in it and no way to get anybody in it except SQL, which
	// is the answer that means the feature is missing.
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"initech","name":"Initech","host":"`+initechHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s for initech = %d %s, want 201", tenantPath, code, body)
	}
	initechID := uuid.MustParse(field(t, body, "id"))
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath+"/"+initechID.String()+"/invite",
		`{"email":"root@initech.localhost","displayName":"Root"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST the first administrator of initech = %d %s, want 201", code, body)
	}
	var first string
	eventually(t, "initech's first administrator to be mailed a link", func() bool {
		for _, sent := range box.Sent() {
			if sent.To == "root@initech.localhost" {
				first = sent.Body
				return true
			}
		}
		return false
	})
	// On initech's own host, not acme's and not the application's: the operator
	// invited them from acme's host, and the link they follow is theirs.
	if !strings.Contains(first, "http://"+initechHost+"/auth/reset") {
		t.Errorf("the first administrator's link is not initech's own host:\n%s", first)
	}
	// No password crossed the control plane: the operator chose none, and this
	// is where one is chosen.
	if code, body = do(t, cfg, nil, http.MethodPost, initechHost, "/api/v1/auth/password/reset",
		`{"token":"`+tokenIn(t, first)+`","new":"a chosen passphrase for initech"}`); code != http.StatusOK {
		t.Fatalf("the first administrator's reset = %d %s, want 200", code, body)
	}
	boss := signIn(t, cfg, initechHost, "root@initech.localhost", "a chosen passphrase for initech")
	// They administer their own tenant: listing its people needs user:read,
	// which needs the admin role the invitation granted.
	if code, body = do(t, cfg, boss, http.MethodGet, initechHost, usersPath, ""); code != http.StatusOK {
		t.Errorf("GET %s as initech's first administrator = %d %s", usersPath, code, body)
	}
	// And nobody else's. The control plane handed over a tenant, not the
	// installation — and at initech's host the control plane is not served at all,
	// which is the stronger of the two answers and the one the surface gives.
	if code, body = do(t, cfg, boss, http.MethodGet, initechHost, tenantPath, ""); code != http.StatusNotFound {
		t.Errorf("initech's administrator reached the control plane = %d %s, want 404", code, body)
	}

	// Inviting somebody is the loop this stage closes: the user module creates
	// a person with no password and publishes user.invited, the auth module
	// subscribes to that, issues a one-time token and mails the link itself —
	// the one message in this application that must not become a row on its way
	// out. Nothing in the user module knows any of that happens.
	invite(t, cfg, admin, "grace@acme.localhost")
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
	// The link carries the tenant's own host, not the application's public one:
	// every tenant is reached at its own name, so a link built from
	// server.public_host would send one customer's people to a front door that
	// is not theirs — and to a sign-in page their session does not answer at.
	if !strings.Contains(link, "http://"+acmeHost+"/auth/reset") {
		t.Errorf("the invitation link is not acme's own host:\n%s", link)
	}
	if strings.Contains(link, cfg.Server.PublicHost) {
		t.Errorf("the invitation link carries the application's public host:\n%s", link)
	}
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
		Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate, Log: quiet(),
		// The installation is where the control plane is served, and this is the
		// composition's own answer to that: the same value boot passes.
		Installation: cfg.Server.InstallationHost,
	})
	for _, m := range c.modules {
		if m.Routes != nil {
			m.Routes(api.Surfaces(m.Name))
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

// knownUnservedNav is the debt this gate is adopted against: two nav entries
// this repository declares and does not serve yet, each with the reason it is
// not a line of work this gate can force.
//
// Neither is a mistake to delete — both are promised by their own module's
// README — and neither can be served the way the roles screen was. The real
// blocker is the same for both and is not in either module: there is no
// read-only generated screen. ui/screens.Mount registers create, edit and
// delete unconditionally and takes Resource.WriteAuth(), and httpx.Permission("")
// panics at the mount site, so a module whose rows are read-only cannot
// register a resource and get its list and detail from the generator. Giving
// httpx.Resource and ui/screens a read-only mode serves both of these from the
// generator and takes this map with it; a seventh and eighth hand-written page
// in the shell would not.
//
// A name is removed from this map by serving the entry, never by moving an
// entry into it. The gate below refuses a stale name for exactly that reason.
var knownUnservedNav = map[string]string{
	"/app/file/files":   "modules/file declares it; no read-only generated screen exists yet",
	"/app/audit/events": "modules/audit declares it; no read-only generated screen exists yet",
}

// TestEveryNavEntryLeadsSomewhere is the same question the shell asks at boot,
// asked where a failure stops a release rather than scrolling past in a log.
//
// modules/admin logs one warning per unserved entry and carries on, which is
// right for a running installation — a broken sidebar link must not be an
// outage — and useless as a gate: the entry that names a screen nobody wrote
// ships, and every operator sees a menu item that leads to a 404. That is how
// /admin/auth/roles survived from the commit that introduced it until a
// consumer's own gate found it.
//
// The composition is mounted in its own order, the shell last, so api.Recorded()
// holds every GET the application answers, generated screens and hand-written
// pages alike; page.Navigation decides the rest, so this gate and the shell
// cannot disagree about what "served" means.
func TestEveryNavEntryLeadsSomewhere(t *testing.T) {
	_, cfg := configure(t)
	_, conn := dbtest.Schema(t)
	c := compose(cfg)
	api, _ := httpx.New(httpx.Options{
		PublicHost: cfg.Server.PublicHost, Docs: true, Tenants: c.tenants, Conn: conn,
		Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate, Log: quiet(),
		Installation: cfg.Server.InstallationHost,
	})
	var entries []module.NavEntry
	for _, m := range c.modules {
		entries = append(entries, m.Nav...)
		if m.Routes != nil {
			m.Routes(api.Surfaces(m.Name))
		}
	}
	if len(entries) == 0 {
		t.Fatal("no module declares a nav entry; this gate would pass an empty application")
	}
	nav := page.NewNavigation(entries, page.Served(api.Recorded()), api.Required())
	unserved := map[string]bool{}
	for _, e := range nav.Unserved() {
		unserved[e.Screen] = true
		if _, known := knownUnservedNav[e.Screen]; !known {
			t.Errorf("the nav entry %q leads to %s, which no route serves: either serve the screen or drop the entry",
				e.Label, e.Screen)
		}
	}
	// And the debt shrinks only by being paid. A path that is served again is
	// removed from the map here, in the change that served it, so the list
	// cannot quietly outlive the reason for it.
	for screen, why := range knownUnservedNav {
		if !unserved[screen] {
			t.Errorf("%s is served now (%s): remove it from knownUnservedNav", screen, why)
		}
	}
}

// The operator's tenant API and switcher open a separate system transaction
// while authentication retains the request's tenant transaction.
func TestWebPoolLeavesRoomForControlPlaneTransactions(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns = new(2), new(0)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{
		Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate,
		Role: app.Web, Transports: transports(), Log: quiet(),
	})
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	admin.Timeout = 3 * time.Second
	for _, path := range []string{tenantPath, "/app/tenant/tenants"} {
		if code, body := do(t, cfg, admin, http.MethodGet, acmeHost, path, ""); code != http.StatusOK || !strings.Contains(body, "Acme Corporation") {
			t.Fatalf("two-connection web pool GET %s = %d %s", path, code, body)
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
		Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate,
		Role: app.Worker, Transport: memory.New(), Log: quiet(),
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

// invite creates somebody with no password, over the wire.
//
// It used to reach for the service main holds, because the module mounted no
// route for inviting: POST to the users collection publishes user.user.created,
// which nothing subscribes to, so an administrator who used it made a person
// who existed, could not sign in and was never told. There is a route now, and
// a test that drives the flow through it is the one that would notice if it
// went away.
func invite(t *testing.T, cfg config.Config, client *http.Client, email string) {
	t.Helper()
	code, body := do(t, cfg, client, http.MethodPost, acmeHost, invitePath,
		`{"email":"`+email+`","displayName":"Grace"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s for %s = %d %s, want 201", invitePath, email, code, body)
	}
	if !strings.Contains(body, `"status":"invited"`) {
		t.Errorf("the invited user is %s, want one who cannot sign in yet", body)
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

// putFile uploads one multipart form with one file part in it, which is the
// shape the file module's upload route takes: the bytes are streamed to storage
// as they arrive, so there is no JSON body anywhere in this.
func putFile(t *testing.T, cfg config.Config, client *http.Client, path, name, contentType, body string) (int, string) {
	t.Helper()
	var form bytes.Buffer
	w := multipart.NewWriter(&form)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+name+`"`)
	header.Set("Content-Type", contentType)
	part, err := w.CreatePart(header)
	if err != nil {
		t.Fatalf("build the form: %v", err)
	}
	if _, err := part.Write([]byte(body)); err != nil {
		t.Fatalf("write the part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close the form: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+cfg.Server.Addr+path, &form)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = acmeHost
	req.Header.Set("Content-Type", w.FormDataContentType())
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(out)
}

// blobs is how many files are under the directory this installation stores
// uploads in. It is the only way to see that the bytes went, because nothing in
// the API answers for them once the row is gone.
func blobs(t *testing.T, cfg config.Config) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(cfg.Files.Dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", cfg.Files.Dir, err)
	}
	return count
}

// TestASlowUploadIsCutOffAndHoldsNoTransaction is the review's byte-a-second
// upload, as a test, and it proves both halves of the fix.
//
// There was no http.Server.ReadTimeout at all, so a client that never finished
// its body was never cut off; and the file module opened the request's
// transaction before it read a byte, so the connection that client pinned was a
// database connection. Sixteen of those is a replica that serves nobody.
//
// The transaction half is measured from outside, through pg_stat_activity,
// because the claim is about a server-side connection and not about a Go value.
// dbtest names this installation's pool after the test's schema, so the count is
// this application's backends and nobody else's. The question it asks is the age
// of the oldest open transaction rather than how many there are: the application
// is running its periodic jobs too, and a job's transaction is milliseconds old,
// while a pinned one keeps getting older for as long as the body trickles.
func TestASlowUploadIsCutOffAndHoldsNoTransaction(t *testing.T) {
	const timeout = 3 * time.Second
	path, cfg := configure(t)
	install(t, path)
	// The only thing this test changes about the reference configuration, and
	// the key exists so that a deployment whose uploads are slower can.
	cfg.Server.ReadTimeout = timeout

	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{
		Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate,
		Role: app.All, Transport: memory.New(), Log: quiet(),
	})
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// The owner connection this installation was migrated with. It is taken
	// from the configuration rather than from dbtest a second time, because
	// asking dbtest for the URLs again would give this test a fresh schema and
	// take the tables out from under the application that is serving.
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	longest := func() float64 {
		var age float64
		const q = `SELECT COALESCE(EXTRACT(EPOCH FROM max(now() - xact_start)), 0) FROM pg_stat_activity
			WHERE state = 'idle in transaction' AND application_name = current_setting('search_path')`
		if err := owner.QueryRowContext(t.Context(), q).Scan(&age); err != nil {
			t.Errorf("read pg_stat_activity: %v", err)
		}
		return age
	}

	// A multipart form whose file part never ends: one byte, then a pause,
	// forever. Nothing declares a length, so this goes out chunked and the
	// server has nothing to read ahead to.
	ctx, cancel := context.WithTimeout(t.Context(), timeout*3)
	var work sync.WaitGroup
	defer work.Wait()
	defer cancel()
	body, out := io.Pipe()
	defer body.Close()
	defer out.Close()
	form := multipart.NewWriter(out)
	work.Go(func() {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="file"; filename="slow.bin"`)
		header.Set("Content-Type", "application/octet-stream")
		part, err := form.CreatePart(header)
		if err != nil {
			_ = out.CloseWithError(err)
			return
		}
		for {
			if _, err := part.Write([]byte("x")); err != nil {
				return
			}
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
				return
			}
		}
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+cfg.Server.Addr+filesPath, body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = acmeHost
	req.Header.Set("Content-Type", form.FormDataContentType())

	type answer struct {
		code int
		err  error
	}
	done := make(chan answer, 1)
	start := time.Now()
	deadline := time.After(timeout * 2)
	work.Go(func() {
		res, err := admin.Do(req)
		if err != nil {
			done <- answer{err: err}
			return
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)
		done <- answer{code: res.StatusCode}
	})

	// While the body trickles, nothing of this installation is holding a
	// transaction. Sampled over the first three quarters of the timeout, so the
	// loop is finished before the server gives up and the handler's own failure
	// path runs.
	samples, oldest := 0, 0.0
	for time.Since(start) < timeout*3/4 {
		select {
		case got := <-done:
			t.Fatalf("upload ended before the read deadline: status=%d error=%v after %s", got.code, got.err, time.Since(start))
		default:
		}
		oldest = max(oldest, longest())
		samples++
		time.Sleep(20 * time.Millisecond)
	}
	if samples < 10 {
		t.Fatalf("only %d samples were taken while the body trickled", samples)
	}
	if oldest > 0.5 {
		t.Errorf("a transaction of this installation was %.2fs old while the body trickled; the bytes are stored before the transaction is opened, so a slow client has nothing to hold",
			oldest)
	}

	var got answer
	select {
	case got = <-done:
	case <-deadline:
		t.Fatalf("the trickled upload did not stop within %s", timeout*2)
	}
	took := time.Since(start)
	// Cut off. The server closed the connection on its deadline, which reaches
	// the client as a 500 the handler produced when its body stopped arriving,
	// or as a failed request; what matters is that it ended, and that it ended
	// when the deadline said rather than whenever the client felt like stopping.
	if got.err == nil && got.code != http.StatusInternalServerError {
		t.Errorf("a body that never ends was answered %d after %s", got.code, took)
	}
	if took > timeout*2 {
		t.Errorf("the trickled upload ran for %s; server.read_timeout is %s", took, timeout)
	}
}

// legacyLayout is every migration the composition now splits across owners,
// under the one owner the foundation used before each module took its own SQL.
// It is built from the same bytes the modules ship, so the ledger it writes is
// the ledger a release before this change actually left behind: same versions,
// same names, same checksums.
func legacyLayout(t *testing.T, sources []db.MigrationSource) db.MigrationSource {
	t.Helper()
	all := fstest.MapFS{}
	for _, source := range sources {
		entries, err := fs.ReadDir(source.Files, ".")
		if err != nil {
			t.Fatalf("read %s: %v", source.Owner, err)
		}
		for _, entry := range entries {
			body, err := fs.ReadFile(source.Files, entry.Name())
			if err != nil {
				t.Fatalf("read %s/%s: %v", source.Owner, entry.Name(), err)
			}
			if _, clash := all[entry.Name()]; clash {
				t.Fatalf("two owners ship %s; the old layout had one of each", entry.Name())
			}
			all[entry.Name()] = &fstest.MapFile{Data: body}
		}
	}
	return db.MigrationSource{Owner: "platformkit", Files: all}
}

// TestAnInstallationFromBeforeModulesOwnedTheirSQLUpgradesInPlace is the
// upgrade half of the migration move, and the half a fresh database cannot
// check: an existing installation's ledger says "platformkit" applied all
// twenty-four files, and the release that splits them must neither refuse the
// files the foundation no longer ships nor run the module files again.
//
// Success alone proves the SQL did not re-run — a second 000004_task.up.sql
// would fail on CREATE TABLE tasks — and the applied_at comparison proves the
// rows were re-owned rather than replaced.
func TestAnInstallationFromBeforeModulesOwnedTheirSQLUpgradesInPlace(t *testing.T) {
	path, cfg := configure(t)
	sources := app.MigrationSources(compose(cfg).modules)
	if err := db.Migrate(t.Context(), cfg.Database.MigrateURL, legacyLayout(t, sources)); err != nil {
		t.Fatalf("the release before this one: %v", err)
	}
	admin := dbtest.Open(t, cfg.Database.MigrateURL)
	before := map[int64]string{}
	ledger := func(into map[int64]string, query string) {
		t.Helper()
		rows, err := admin.QueryContext(t.Context(), query)
		if err != nil {
			t.Fatalf("read the ledger: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var version int64
			var value string
			if err := rows.Scan(&version, &value); err != nil {
				t.Fatalf("read the ledger: %v", err)
			}
			into[version] = value
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read the ledger: %v", err)
		}
	}
	ledger(before, "SELECT version, applied_at::text FROM schema_migrations")
	// 24 became 25 when modules/user/000025 added the handle column, and 25
	// became 26 when the kernel added 000026_module_schema. The number is
	// the point of the assertion: an upgrade fixture that silently stopped counting
	// a migration would pass while upgrading a real installation past a file it
	// should have applied, so a new migration has to arrive here and say so.
	if len(before) != 26 {
		t.Fatalf("the old layout applied %d files, want 26", len(before))
	}

	// The new release, through the path a person runs: bootstrap migrates with
	// the composed sources before it writes the first tenant.
	install(t, path)

	owners := map[int64]string{}
	ledger(owners, "SELECT version, owner FROM schema_migrations")
	after := map[int64]string{}
	ledger(after, "SELECT version, applied_at::text FROM schema_migrations")
	if len(owners) != len(before) {
		t.Fatalf("the upgrade left %d applied files, want the same %d", len(owners), len(before))
	}
	for version, when := range before {
		if after[version] != when {
			t.Errorf("version %d was applied again: %s became %s", version, when, after[version])
		}
	}
	// Each file now reads under the owner that ships it.
	want := map[int64]string{}
	for _, source := range sources {
		entries, err := fs.ReadDir(source.Files, ".")
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			version, err := strconv.ParseInt(strings.SplitN(entry.Name(), "_", 2)[0], 10, 64)
			if err != nil {
				t.Fatalf("%s is not <version>_<name>.up.sql", entry.Name())
			}
			want[version] = source.Owner
		}
	}
	for version, owner := range want {
		if owners[version] != owner {
			t.Errorf("version %d reads as %q, want %q", version, owners[version], owner)
		}
	}
	// And the application built on that schema serves.
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{
		Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate,
		Role: app.All, Transport: memory.New(), Log: quiet(),
	})
	admins := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	if code, body := do(t, cfg, admins, http.MethodGet, acmeHost, tasksPath, ""); code != http.StatusOK {
		t.Fatalf("the upgraded installation's task list = %d %s", code, body)
	}
}

// TestPinnedAddresses is why a deployment may name an address the kernel
// composed. The failure page is rendered before any module has a chance to answer,
// so it holds the workspace root, the sign-in page and the stylesheet prefix as
// literals (see fault.go), and the admin shell's form posts to the auth module's
// door by the same kind of pin. A pin that drifts is a failure page that links
// nowhere, which is discovered by the one person who least needs it: whoever is
// already looking at an outage. So the pins are asked of the running server here.
func TestPinnedAddresses(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{
		Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate,
		Role: app.Web, Transport: memory.New(), Log: quiet(),
	})

	// The workspace root answers something — a document, or the redirect to sign
	// in — and not the 404 an address nobody claimed gives.
	// What the root answers an anonymous caller depends on what the caller says
	// it is — a browser is sent to the sign-in page, anything else is refused the
	// way an API refuses — and this helper's client says nothing either way. Both
	// are answers; a 404 is not, because the root is claimed.
	if code, body := do(t, cfg, nil, http.MethodGet, acmeHost, pinnedWorkspace, ""); code == http.StatusNotFound {
		t.Errorf("the workspace root %s = 404; the first module in composition order claims it: %s", pinnedWorkspace, body)
	}
	if code, body := do(t, cfg, nil, http.MethodGet, acmeHost, pinnedSignIn, ""); code != http.StatusOK ||
		!strings.Contains(body, "data-login-form") {
		t.Errorf("the sign-in page %s = %d, want the form the shell is named for", pinnedSignIn, code)
	}
	// The stylesheet, because the fault page references it by prefix and nothing
	// else checks that the prefix is where the files are.
	if code, body := do(t, cfg, nil, http.MethodGet, acmeHost, pinnedAssets+"/app.css", ""); code != http.StatusOK {
		t.Errorf("the shell's stylesheet at %s/app.css = %d %s", pinnedAssets, code, body)
	}
	// And the auth door the login form posts to: refused for the right reason is
	// the proof that the address is the door rather than a hole near it.
	if code, body := do(t, cfg, nil, http.MethodPost, acmeHost, pinnedSignInAPI,
		`{"email":"nobody@acme.localhost","password":"not the passphrase"}`); code != http.StatusUnauthorized {
		t.Errorf("POST %s with a wrong passphrase = %d %s, want the refusal the door gives", pinnedSignInAPI, code, body)
	}
	// The catalog, which is the kernel's own route and the whole reason a native
	// client can be built against this installation at all.
	if code, _ := do(t, cfg, nil, http.MethodGet, acmeHost, "/api/v1/app/resources", ""); code == http.StatusNotFound {
		t.Error("the workspace catalog is not mounted, so a native shell has nothing to read")
	}
	// The public file door, which the site's own markup links. An id that is not
	// an id is refused by the door's own shape check, and that answer — not a 404 —
	// is what says something is mounted behind the pin: TestThePublicPageLinksOnly
	// AddressesTheInstallationServes asks the same address of a file that exists.
	if code, body := do(t, cfg, nil, http.MethodGet, acmeHost, pinnedPublicFile+"/not-an-id", ""); code == http.StatusNotFound {
		t.Errorf("the public file door %s/%s = 404; the site links it for its logo: %s", pinnedPublicFile, "not-an-id", body)
	}
}

// TestAWriteOfAResourceWrittenOnTheControlPlaneIsTurnedTowardsItsDoor is the
// reference composition's share of the split. modules/billing's plan catalog is the
// one resource this installation reads on the workspace and writes on the control
// plane — the arrangement the catalog had to start describing (its entry names
// write_path) and the arrangement a derived client reads as an ordinary writable
// resource, because every writable resource it has ever met was one. So the case is
// written as that client writes it: read the document, then post to the address the
// entry's own path gives.
//
// Three things are owed there. The write is refused, and a refused write reaches no
// handler, publishes nothing and leaves no row. The refusal carries the way out,
// because "this address does not accept POST" is also the sentence an address that
// never heard of plans says. And the installation's own address is not spoken of at a
// host that serves no control plane — the answer a caller gets about that surface is
// the same at every host that does not serve it, which is the rule the host gate
// exists to keep.
func TestAWriteOfAResourceWrittenOnTheControlPlaneIsTurnedTowardsItsDoor(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{
		Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate,
		Role: app.All, Transport: memory.New(), Log: quiet(),
	})

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// The document says what the arrangement is, to the caller who may write.
	_, doc := do(t, cfg, admin, http.MethodGet, acmeHost, "/api/v1/app/resources", "")
	var catalog struct {
		Resources []struct {
			Path      string `json:"path"`
			Writable  bool   `json:"writable"`
			WritePath string `json:"write_path"`
		} `json:"resources"`
	}
	if err := json.Unmarshal([]byte(doc), &catalog); err != nil {
		t.Fatalf("the catalog is not the document a shell parses: %v\n%s", err, doc)
	}
	seen := false
	for _, e := range catalog.Resources {
		if e.Path != plansPath {
			continue
		}
		seen = true
		if !e.Writable || e.WritePath != plansWrite {
			t.Errorf("the catalog's %s entry says writable=%v write_path=%q, want writable and %s",
				e.Path, e.Writable, e.WritePath, plansWrite)
		}
	}
	if !seen {
		t.Fatalf("the catalog carries no %s entry: %s", plansPath, doc)
	}

	// What stands behind the door is read through the door that reads it, rather
	// than trusted from the body of a refusal.
	total := func() int {
		code, body := do(t, cfg, admin, http.MethodGet, acmeHost, plansPath, "")
		if code != http.StatusOK {
			t.Fatalf("the plan list = %d %s", code, body)
		}
		var page struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatalf("the plan list is not a page: %v\n%s", err, body)
		}
		return len(page.Items)
	}
	before := total()
	if before == 0 {
		if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, plansWrite,
			`{"code":"pro","name":"Pro","priceCents":2900,"currency":"EUR","interval":"month","active":true}`); code != http.StatusCreated {
			t.Fatalf("the plan this case counts = %d %s", code, body)
		}
		before = total()
	}

	id := uuid.New()
	for _, ask := range []struct{ method, at, named string }{
		{http.MethodPost, plansPath, plansWrite},
		{http.MethodPatch, plansPath + "/" + id.String(), plansWrite + "/" + id.String()},
	} {
		code, body := do(t, cfg, admin, ask.method, acmeHost, ask.at,
			`{"code":"wrong","name":"Wrong","priceCents":1,"currency":"EUR","interval":"month","active":true}`)
		switch {
		case code != http.StatusForbidden:
			t.Errorf("%s %s = %d %s, want the refusal that names the door", ask.method, ask.at, code, body)
		case !strings.Contains(body, httpx.CodeWriteElsewhere), !strings.Contains(body, ask.named):
			t.Errorf("%s %s = %d %s, want %s and the address %s", ask.method, ask.at, code, body, httpx.CodeWriteElsewhere, ask.named)
		}
	}
	if got := total(); got != before {
		t.Errorf("the refused writes left %d plans where there were %d", got, before)
	}

	// The same ask where no control plane is served is the answer every unmounted
	// address gets there, and it says nothing of the installation's own address.
	// Globex's administrator holds the billing write inside their own tenant; it
	// buys them no map of the surface they are not served at.
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("create globex = %d %s", code, body)
	}
	provision(t, cfg, uuid.MustParse(field(t, body, "id")), "root@globex.localhost")
	globex := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)
	if code, body := do(t, cfg, globex, http.MethodPost, globexHost, plansPath, `{"currency":"EUR"}`); code != http.StatusMethodNotAllowed ||
		strings.Contains(body, "/api/v1/ops") {
		t.Errorf("POST %s where no control plane is served = %d %s, want the 405 that host gives anybody and no word of %s",
			plansPath, code, body, plansWrite)
	}
}
