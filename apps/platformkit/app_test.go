package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math/rand/v2"
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
	"github.com/septagon-oss/platformkit/kit/cache"
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
	"github.com/septagon-oss/platformkit/modules/notification/providers/gomail/gomailtest"
	"github.com/septagon-oss/platformkit/modules/task"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/ui/page"
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
	// acmeName is the tenant's own name, which the bootstrap's operator typed. It is
	// a value and not copy: no catalogue can hold it, and the frame puts it in the
	// tab title and the chrome, so the pseudo-locale gate reads it back as data.
	acmeName = "Acme Corporation"
)

// quiet keeps a passing test's output to the test's own lines.
func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

// fixtureLockTimeout is the lock budget this product's own fixtures ask their migration
// for, and it is not the runner's default. The reason is one database: `make check` runs
// every package here against the Postgres this repository's stack starts, so a migration
// meets the transactions the packages beside it hold open, and
// modules/notification/000030's concurrent index build waits for all of them whatever
// table they touched. A deployment wants the five-second default — a boot that cannot get
// a lock should stop and say so — and kit/app/budget_from_configuration_test.go
// and kit/db/migrate_expand_contract_test.go keep that answer pinned, one through the
// configuration and one through the runner. A fixture that stops reports a schema it was
// never given, which is why kit/db/dbtest gives its own migration the same patience.
const fixtureLockTimeout = 90 * time.Second

// webhookKey is the composition's webhook secret key: thirty-two bytes, the width
// AES-256-GCM asks for. A fixture's value, not a secret — no receiver on this
// planet verifies a delivery signed with it.
const webhookKey = "0123456789abcdef0123456789abcdef"

// configure writes the configuration the reference app would have read from
// config.yaml, against an installation of this test's own, and returns the path and
// the loaded value.
//
// The installation arrives as a copy the server made of the one this package's TestMain prepared,
// which is what a case used to buy by running the whole migration and the bootstrap for itself: the
// copy holds the same ledger, the same tenant and the same administrator, and it holds whatever the
// case writes for itself alone in a database no other case can reach. A case whose own claim is
// about installing — the bootstrap's answer, the ledger, a refusal only an untouched database can
// give — takes configureFresh rather than a copy of a finished installation.
func configure(t *testing.T) (string, config.Config) {
	t.Helper()
	t.Setenv("PLATFORMKIT_SEED_OPERATOR_EMAIL", adminEmail)
	t.Setenv("PLATFORMKIT_SEED_OPERATOR_PASSWORD", adminPass)
	migrateURL, appURL := dbtest.Clone(t)
	path, cfg := writeConfig(t, migrateURL, appURL)
	copied.Store(t, struct{}{})
	t.Cleanup(func() { copied.Delete(t) })
	return path, cfg
}

// configureFresh is configure against an empty schema of this test's own: no tables, no ledger, no
// tenant, nothing. It is the whole cost of an installation, and it belongs to the cases for which
// getting there is the thing being asserted — see install.
func configureFresh(t *testing.T) (string, config.Config) {
	t.Helper()
	// The seed command's operator proves themselves the way every deployment
	// does — through the environment, never argv — and the person they prove they
	// are is the administrator install below creates, in the installation tenant
	// this configuration names at acme's host. The case that checks the refusal of
	// an absent credential empties both variables for itself.
	t.Setenv("PLATFORMKIT_SEED_OPERATOR_EMAIL", adminEmail)
	t.Setenv("PLATFORMKIT_SEED_OPERATOR_PASSWORD", adminPass)
	migrateURL, appURL := dbtest.URLs(t)
	return writeConfig(t, migrateURL, appURL)
}

// copied names the cases that were handed a copy of the prepared installation. install reads it,
// and nothing else does.
var copied sync.Map // *testing.T → struct{}

// writeConfig writes config.yaml for one installation and loads what it wrote. The two URLs name
// the database and the namespace it lives in; everything else here is this product's own answer to
// what a reference installation is configured with.
//
// The file module keeps its bytes under a directory of this test's own, so a suite that uploads
// something leaves nothing behind and two suites running at once do not share a disk. The
// installation is reached at acme's host, which is the operator's own tenant (the bootstrap created
// it): the control plane is served where the installation is, and nowhere else.
// TestTheControlPlaneIsNotFoundAtATenantHost in kit/httpx is where globex's answer at the same
// address is proved.
func writeConfig(t *testing.T, migrateURL, appURL string) (string, config.Config) {
	t.Helper()
	path := t.TempDir() + "/config.yaml"
	body := "server:\n  addr: \"" + freeAddr(t) + "\"\n  public_host: \"platformkit.localhost\"\n  installation_host: \"" + acmeHost + "\"\n  docs: true\n" +
		"database:\n  url: \"" + appURL + "\"\n  migrate_url: \"" + migrateURL + "\"\n  lock_timeout: \"" + fixtureLockTimeout.String() + "\"\n" +
		"nats:\n  url: \"nats://localhost:4222\"\n" +
		"log:\n  level: \"error\"\n" +
		"files:\n  dir: \"" + t.TempDir() + "\"\n" +
		// Outbound webhooks are composed, so the composition needs the key that
		// wraps an endpoint's signing secret. The fixture's own thirty-two bytes.
		"webhook:\n  secret_key: \"" + webhookKey + "\"\n" +
		"auth:\n  oidc:\n    issuer: \"\"\n    client_id: \"\"\n    client_secret: \"\"\n    redirect_path: \"\"\n"
	// A case that asked for the installation with a mail host gets one, pointed at
	// the relay on loopback; every other case gets the recording carrier it always
	// got, which is what a deployment that names no host actually runs.
	if relay, ok := mailing.Load(t); ok {
		body += mailSection(relay.(*gomailtest.Relay))
	}
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
// does: an empty database in, a tenant and an administrator out. It does
// nothing for a case configure gave a copy of the prepared installation, because
// the bootstrap is what made that copy; a case that needs the run itself — one
// that asserts the bootstrap's own answer, the ledger, or a refusal only an
// untouched database can give — takes configureFresh, and this answers it in
// full.
//
// A run the database declined for contention is run again, because that is what db.ErrContended
// says instead of failing: it may be run again, with the files it applied already there to be read.
// The decline is ordinary in this package's own database, which holds nothing but the compositions
// this suite installs, each of them taking the composition's advisory lock and every catalog write
// that comes with it; under -race the queue behind that lock passes the five seconds a deployment
// chose for itself, and CI job 57346 reported one case's bootstrap as a failing test over exactly
// that sentence. Running it again is the operator's door (apps/platformkit/migrate.go) standing in
// the place the operator occupies here, and the production budget stays the five seconds it is, which
// is why the configuration above names no longer patience. A migration refused for any other reason
// ends the case as it always did, and the waiting is bounded: a decline that outlasts it is reported
// with the count of runs it took.
func install(t *testing.T, path string) {
	t.Helper()
	if _, ok := copied.Load(t); ok {
		// The configuration names a database the server copied from the prepared installation, so
		// the migration and the bootstrap this case would have run are what made that copy. What is
		// left for this case to do is write into it.
		return
	}
	t.Setenv("PLATFORMKIT_BOOTSTRAP_PASSWORD", adminPass)
	args := installationArgs(path)
	until := time.Now().Add(installContentionWait)
	for runs := 1; ; runs++ {
		err := bootstrap(args)
		if err == nil {
			return
		}
		if !errors.Is(err, db.ErrContended) {
			t.Fatalf("bootstrap: %v", err)
		}
		if time.Now().Add(installContentionPause).After(until) {
			t.Fatalf("bootstrap: %v (after %d runs the database declined for contention)", err, runs)
		}
		t.Logf("bootstrap run %d was declined for contention, so it is run again: %v", runs, err)
		time.Sleep(installContentionPause)
	}
}

// The two waits install's retry keeps. A declined run left what it applied behind, so the pause only
// waits for the queue in front of it to move; the bound is what stops a suite waiting on a database
// that has stopped answering.
const (
	installContentionPause = 250 * time.Millisecond
	installContentionWait  = 2 * time.Minute
)

// installationArgs are the answers bootstrap takes from an operator standing in front of an empty
// installation: the tenant's slug, host and name, its first administrator, and the languages its
// people are served in. One spelling, because the package's TestMain prepares the installation every
// case is copied from, and a case must never be looking at a different installation than its
// neighbours.
//
// The installation's second language is one this tenant's people are served in, said at the same
// moment the tenant is created. A tenant is not served in it by accident of the catalogues somebody
// compiled: SetLocale is the declaration, and a bootstrap that says nothing leaves the tenant in the
// one language its copy is written in — which is what apps/platformkit/locale_test.go and
// e2e/localization.spec.ts then check the other language against.
func installationArgs(path string) []string {
	return []string{
		"--config", path, "--tenant", "acme", "--host", acmeHost,
		"--name", acmeName, "--admin-email", adminEmail,
		"--language", "pt-PT",
	}
}

// start runs the application in the background and returns when it is listening.
//
// A case that does not name a catalog renderer gets the product's own mount —
// apps/platformkit/fault.go's workspaceCatalog, not a second renderer written here:
// the composition gate refuses a composition that registers resources and renders no
// workspace document, and a test that skipped it would be starting something the
// product would never boot. A copy here is what let a contract gate pass while the
// shipped binary described its catalog as an empty object.
func start(t *testing.T, cfg config.Config, mods []module.Module, opts app.Options) {
	t.Helper()
	if opts.WorkspaceCatalog == nil {
		opts.WorkspaceCatalog = workspaceCatalog(nil)
	}
	// Same for the installation's host: the control plane is served where the
	// installation is reached, and a test that left it out would be starting a
	// process that answers its own operator at no address at all.
	if opts.Installation.Host == "" {
		opts.Installation = app.Installation{Host: cfg.Server.InstallationHost}
	}
	// The wait at the end asks this application what it is serving rather than
	// asking the address, so it needs the application's own log: this wraps the
	// logger the case named instead of replacing it, and every record still goes
	// where the case said it should.
	listening := make(chan struct{})
	var watched slog.Handler = slog.DiscardHandler
	if opts.Log != nil {
		watched = opts.Log.Handler()
	}
	opts.Log = slog.New(&servingHandler{addr: cfg.Server.Addr, inner: watched, ready: listening})
	a, err := app.New(t.Context(), cfg, mods, opts)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	var runErr error
	go func() {
		defer close(stopped)
		runErr = a.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
		if runErr != nil {
			t.Errorf("Run: %v", runErr)
		}
	})
	waitServing(t, cfg.Server.Addr, listening, stopped, &runErr)
}

// TestAnEmptyDatabaseBecomesAWorkingInstallation is the README's five commands
// as a test, and gate 9: nothing exists, the bootstrap creates the first tenant
// and its administrator, the process migrates and serves, and the whole round
// trip — sign in, create, list, sign out — happens over a cookie.
func TestAnEmptyDatabaseBecomesAWorkingInstallation(t *testing.T) {
	path, cfg := configureFresh(t)
	install(t, path)

	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{
		Tenants: c.testTenants(), Authorize: c.testAuth(), Entitle: c.plans, Authenticate: c.testAuth().Authenticate,
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
	if code, _ := do(t, cfg, nil, http.MethodGet, acmeHost, tasksPath, ""); code != http.StatusUnauthorized {
		t.Errorf("an anonymous list = %d, want 401", code)
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
	// with no session at all — which is what publishing means. Raw HTML is
	// refused on write, before it can become a published page.
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, contentPath,
		`{"slug":"unsafe","title":"Unsafe","body":"<script>alert(1)</script>"}`); code != http.StatusUnprocessableEntity || !strings.Contains(body, "raw HTML") {
		t.Fatalf("unsafe content = %d %s, want a raw HTML refusal", code, body)
	}
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, contentPath,
		`{"slug":"About Us","title":"About Acme","kind":"page","body":"## About\n\nWe make **things**."}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", contentPath, code, body)
	}
	pageID := field(t, body, "id")
	if code, body = do(t, cfg, nil, http.MethodGet, acmeHost, "/api/v1/content/public/about-us", ""); code != http.StatusNotFound {
		t.Errorf("an unpublished page = %d %s, want 404", code, body)
	}
	// The author is not the publisher: the module refuses the person who wrote the
	// page, by name, and the page goes live when a second holder of the permission
	// asks for it. The installation invites that person here for the same reason it
	// invites everybody else later.
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, contentPath+"/"+pageID+"/publish", ""); code != http.StatusConflict ||
		!strings.Contains(body, "author") {
		t.Fatalf("self-publication = %d %s, want a 409 naming authorship", code, body)
	}
	publisher := signIn(t, cfg, acmeHost, publisherIn(t, cfg, admin, "publisher@acme.localhost"), publisherPass)
	if code, body = do(t, cfg, publisher, http.MethodPost, acmeHost, contentPath+"/"+pageID+"/publish", ""); code != http.StatusOK {
		t.Fatalf("publish = %d %s, want 200", code, body)
	}
	code, body = do(t, cfg, nil, http.MethodGet, acmeHost, "/api/v1/content/public/about-us", "")
	if code != http.StatusOK || !strings.Contains(body, "<strong>things</strong>") {
		t.Fatalf("the published page = %d %s", code, body)
	}
	if strings.Contains(body, "<script") || strings.Contains(body, "alert(1)") {
		t.Errorf("the published page carries unsafe markup:\n%s", body)
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
	if code, _ = do(t, cfg, admin, http.MethodGet, globexHost, "/api/v1/auth/me", ""); code != http.StatusUnauthorized {
		t.Errorf("acme's session at globex = %d, want 401", code)
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
	//
	// Which languages a tenant is served in, and which identity provider its people
	// sign in against, are in this list because they are the newest of these routes
	// and the two a tenant would most like to write for itself: the tenant's own
	// host is exactly where their absence has to show.
	for _, probe := range []struct{ method, path, body string }{
		{http.MethodGet, tenantPath, ""},
		{http.MethodPost, tenantPath, `{"slug":"evil","name":"Evil","host":"evil.localhost"}`},
		{http.MethodPost, tenantPath + "/" + globexID.String() + "/suspend", ""},
		{http.MethodGet, tenantPath + "/" + globexID.String(), ""},
		{http.MethodPost, tenantPath + "/" + globexID.String() + "/hosts", `{"host":"evil.localhost"}`},
		{http.MethodPost, tenantPath + "/" + globexID.String() + "/locale", `{"default":"pt-PT","supported":["en","pt-PT"]}`},
		{http.MethodPost, tenantPath + "/" + globexID.String() + "/oidc", `{"issuer":"https://idp.globex.example","clientId":"platformkit","secretRef":"GLOBEX_OIDC_SECRET"}`},
		{http.MethodPost, tenantPath + "/" + globexID.String() + "/oidc/clear", ""},
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
	if !strings.Contains(first, "http://"+initechHost+"/app/auth/reset") {
		t.Errorf("the first administrator's link is not initech's own host:\n%s", first)
	}
	// No password crossed the control plane: the operator chose none, and this
	// is where one is chosen.
	redeemMailedLink(t, cfg, nil, initechHost, "/api/v1/auth/password/reset",
		`{"token":"`+tokenIn(t, first)+`","new":"a chosen passphrase for initech"}`)
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
	if !strings.Contains(link, "http://"+acmeHost+"/app/auth/reset") {
		t.Errorf("the invitation link is not acme's own host:\n%s", link)
	}
	if strings.Contains(link, cfg.Server.PublicHost) {
		t.Errorf("the invitation link carries the application's public host:\n%s", link)
	}
	// The link is in the box, and the token it carries is spendable already:
	// modules/auth hands the message to the transport only once the transaction that
	// minted the credential has committed (kit/db's AfterCommit), so a mailbox
	// holding a link is a fact about committed rows and there is nothing left to wait
	// for between reading the mail and spending what is in it.
	token := tokenIn(t, link)

	// The link works, once, and it is what turns an invitation into somebody
	// who can sign in.
	redeemMailedLink(t, cfg, nil, acmeHost, "/api/v1/auth/password/reset",
		`{"token":"`+token+`","new":"a chosen passphrase for grace"}`)
	signIn(t, cfg, acmeHost, "grace@acme.localhost", "a chosen passphrase for grace")
	if code, body = do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/auth/password/reset",
		`{"token":"`+token+`","new":"another passphrase entirely"}`); code != http.StatusUnauthorized {
		t.Errorf("the link worked twice = %d %s, want 401", code, body)
	}

	// Signing out ends it: the same cookie is nobody afterwards.
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/auth/logout", ""); code != http.StatusOK {
		t.Fatalf("logout = %d %s, want 200", code, body)
	}
	if code, _ = do(t, cfg, admin, http.MethodGet, acmeHost, "/api/v1/auth/me", ""); code != http.StatusUnauthorized {
		t.Errorf("me after logout = %d, want 401", code)
	}
}

// TestBootstrapRefusesAnInstallationThatAlreadyExists: the one write with no
// caller to authorize is safe because it can only ever happen once.
func TestBootstrapRefusesAnInstallationThatAlreadyExists(t *testing.T) {
	path, _ := configureFresh(t)
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
// included, carries one declaration from the closed set of five.
func TestEveryOperationDeclaresExactlyOneAuthorization(t *testing.T) {
	_, cfg := configure(t)
	_, conn := dbtest.Schema(t)
	c := compose(cfg)
	api, _ := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: cfg.Server.PublicHost, Docs: true, Tenants: c.testTenants(), Conn: conn,
		Authorize: c.testAuth(), Entitle: c.plans, Authenticate: c.testAuth().Authenticate, Log: quiet(),
		// A composition built by hand declares what its manifests say: four modules
		// cross tenants, and the fixture names their four reasons.
		SystemAccess: systemAccessOf(c.modules),
		// The installation is where the control plane is served, and this is the
		// composition's own answer to that: the same value boot passes.
		Installation: cfg.Server.InstallationHost,
	})
	for _, m := range c.modules {
		if m.Routes != nil {
			m.Routes(api.Surfaces(m.Name))
		}
	}
	health.Register(api, []health.Check{health.DatabaseCheck(conn)})
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
		case "public", "signed_in", "any_credential", "bearer_key":
		case "permission", "operator_permission":
			if read.Permission == "" {
				t.Errorf("%s %s requires a permission with no name", op.Method, op.Path)
			}
		default:
			t.Errorf("%s %s declares %q, which is not one of the six", op.Method, op.Path, read.Kind)
		}
		kinds[read.Kind]++
	}
	// Every kind is used, which is what makes the closed set worth having. The
	// fifth, any_credential, is not in this list because it is the composition's
	// own declaration — kit/app mounts the one operation that carries it, at the
	// catalog address, and this gate builds the API from module routes without
	// going through kit/app's boot. What the fifth door is for is pinned where the
	// rule lives, by kit/httpx's scoped-credential cases, and at the live
	// composition by TestAScopedKeyReadsTheCatalogAShellIsBuiltFrom.
	for _, kind := range []string{"public", "signed_in", "permission", "operator_permission", "bearer_key"} {
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

// knownUnservedNav is the debt this gate is adopted against: the nav entries this
// repository declares and does not serve yet, each with the reason it is not a
// line of work this gate can force.
//
// The entry left here is not a mistake to delete — it is promised by its own
// module's README — and it cannot be served the way the roles screen was: there is
// no read-only generated screen. ui/screens.Mount registers create, edit and delete
// unconditionally and takes Resource.WriteAuth(), and httpx.Permission("") panics at
// the mount site, so a module whose rows are read-only cannot register a resource and
// get its list and detail from the generator. Giving httpx.Resource and ui/screens a
// read-only mode serves this entry from the generator and takes the map with it. A
// hand-written page is what modules/file chose instead, because its screen is not a
// resource the generator describes either: a browse of rows, the ledger that says what
// shows each one, and a meter over the sum an upload is refused against.
//
// A name is removed from this map by serving the entry, never by moving an entry
// into it. The gate below refuses a stale name for exactly that reason.
var knownUnservedNav = map[string]string{
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
		Cache:      cache.Memory("pkit"),
		PublicHost: cfg.Server.PublicHost, Docs: true, Tenants: c.testTenants(), Conn: conn,
		Authorize: c.testAuth(), Entitle: c.plans, Authenticate: c.testAuth().Authenticate, Log: quiet(),
		// A composition built by hand declares what its manifests say: four modules
		// cross tenants, and the fixture names their four reasons.
		SystemAccess: systemAccessOf(c.modules),
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
		Tenants: c.testTenants(), Authorize: c.testAuth(), Entitle: c.plans, Authenticate: c.testAuth().Authenticate,
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
		if _, err := c.testTenants().Create(ctx, tx, tenantcontracts.NewTenant{
			Slug: "globex", Name: "Globex", Host: globexHost,
		}); err != nil {
			return err
		}
		all, err := c.testTenants().List(ctx, tx)
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
	mods := []module.Module{task.New(task.Deps{
		Tenants: tenantcontracts.Active{Service: c.testTenants()}, SweepEvery: 200 * time.Millisecond,
	})}
	start(t, cfg, mods, app.Options{
		Tenants: c.testTenants(), Authorize: c.testAuth(), Entitle: c.plans, Authenticate: c.testAuth().Authenticate,
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

// waitForAudit is the newest row the trail holds under this event name, once the
// trail has caught up with everything this composition has already committed.
//
// Waiting for "the first row with this name" waits for the wrong fact, because a
// name is not a row. tenant.lifecycle_recorded is published by every lifecycle
// verb, so a case that creates a customer before it suspends one has its wait
// closed by the create's own mirror row twenty round-trips early, and the
// assertions that follow read the create back as the suspension. Whether that
// happens depends on which side of the relay's one-second tick (kit/app
// relayEvery) the two writes fall on — a coin toss about a scheduler, not a test.
//
// What a case knows at the moment it asks is narrower and enough: it just
// performed a write, that write's outbox row committed with it, and the trail row
// exists once the worker has published that row. The queue is the thing that says
// so. relayBatch publishes a batch and only then stamps it (kit/events/relay.go),
// and Publish on the in-process transport returns only after every sink has
// finished handling the event (kit/events/providers/memory). So while any row is
// still unpublished the trail is filling; the moment none is, every committed
// write has its row. From there the newest row with the name is the row this test
// caused, and the answer stops depending on how fast or slow the tick underneath
// it happened to be.
func waitForAudit(t *testing.T, cfg config.Config, client *http.Client, name string) map[string]any {
	t.Helper()
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open the database to read the queue: %v", err)
	}
	defer conn.Close()

	var row map[string]any
	eventually(t, "the trail to record "+name+" once the queue has delivered what is on it", func() bool {
		if !queueDrained(t, conn) {
			return false
		}
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

// queueDrained reports whether the outbox holds nothing still waiting for the
// relay. It reads the queue the way the relay reads it, in one system
// transaction, because the queue spans every tenant and the row that matters here
// belongs to whichever one a request acted on. An unpublished row is a trail row
// on its way, which is why a read of the trail that means to see the whole of it
// asks this first.
func queueDrained(t *testing.T, conn *db.Conn) bool {
	t.Helper()
	var pending int64
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Table("platformkit_outbox").Where("published_at IS NULL").
			Count(&pending).Error
	})
	if err != nil {
		t.Fatalf("count the outbox rows still unpublished: %v", err)
	}
	return pending == 0
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
		acme, err = c.testTenants().ByHost(ctx, tx, acmeHost)
		return err
	})
	if err != nil {
		t.Fatalf("resolve %s: %v", acmeHost, err)
	}

	var id uuid.UUID
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := c.notify.Notify(ctx, tx, notificationcontracts.Notice{
			Recipient: recipient, Title: "chiller-2 supply temp is out of band",
			Body: "The task is waiting for somebody.", Link: "/admin/task/tasks", Wants: notificationcontracts.WantsEmail,
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

// confirmMailboxLink redeems a confirmation link the composed mailbox is already
// holding, and is how every case here spends a mailed credential.
//
// Waiting for the message is not waiting for the link. The in-memory mailbox has
// it from the moment the transport takes the message, and the credential row
// commits only when the delivery transaction does — a real SMTP server answers
// first, and so does a test double. On a loaded machine that gap is wide enough to
// read "that verification link is invalid or has expired" about a link mailed an
// instant earlier, which is a refusal about the reader's timing rather than the
// account. A 401 consumes nothing — the row is not there to spend — so the answer
// is to ask again until the row is there or the tries are spent, and to fail with
// the last answer when they are.
func confirmMailboxLink(t *testing.T, cfg config.Config, host, link string) {
	t.Helper()
	redeemMailedLink(t, cfg, nil, host, "/api/v1/public/auth/verify-email", `{"token":"`+link+`"}`)
}

// redeemMailedLink is confirmMailboxLink for any route that spends a credential a
// mail carried — the verification route and the set-password route mint their
// tokens in different tables and are mailed by different sends, and both are read
// out of the same mailbox by these cases.
func redeemMailedLink(t *testing.T, cfg config.Config, client *http.Client, host, path, body string) {
	t.Helper()
	code, out := 0, ""
	for attempt := 1; attempt <= 20; attempt++ {
		code, out = do(t, cfg, client, http.MethodPost, host, path, body)
		if code == http.StatusOK || code == http.StatusAccepted {
			return
		}
		if code != http.StatusUnauthorized {
			t.Fatalf("redeeming the mailed link at %s = %d %s", path, code, out)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("redeeming the mailed link at %s = %d %s after twenty attempts, so the credential "+
		"the mailbox was holding never became a row the server would spend", path, code, out)
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
		_, err := c.testUsers().Provision(ctx, tx, tenantID, email, "", adminPass, []string{authcontracts.RoleAdmin})
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

// servingSettle is how long the address has to keep answering this application's
// own liveness probe, and this application has to keep running, before the wait
// calls the boot served. It is short because everything it can be waiting for is
// immediate: a listener either took the address or failed to, within microseconds
// of the sentence.
const servingSettle = 250 * time.Millisecond

// waitServing returns when this application is answering at addr — not when
// something is.
//
// Two failures are answered here, and they are two different ones.
//
// The first is the address. Run migrates before it listens (kit/app, Start then
// serve), and the wait for the composition's advisory lock is bounded only by the
// caller's context (kit/db/migrate.go, holdCompositionLock) — one Postgres, one
// lock, every suite running on this host. While this case's composition was still
// in that queue, the address was one this test had released (see freeAddr) and some
// other process could hold it, and a dial that connected was read as "ready": the
// case then asked a stranger's composition for its own sign-in page and reported the
// 404 it got back as its own failure, and the stranger's shutdown showed up later as
// a refused connection.
//
// The second is that the address cannot answer for *which* application is behind
// it. A stranger running this same binary answers GET /health with 200 too, so a
// probe of the address — however good a question it is — is not a proof that the
// process behind the socket is the one this case started. So the wait is on the
// application: it returns only once the log this composition was given says it is
// listening at this address (servingHandler matches the sentence and the address),
// and the address has kept holding for servingSettle while this application is
// still running. What a run that got there first — a port it could not bind, a gate
// that refused the composition — arrives as is its own error, said at once, instead
// of as a cascade of 404s.
//
// An address that answers somebody else's liveness probe is not this application's
// either. It is not said so at once: a composition that has lost the address loses
// the bind within microseconds of writing the sentence this wait answered to, so the
// nearer truth is Run's own error, and the wait gives it the settle window to say so
// before it prints the stranger's answer.
//
// bootWait bounds the whole thing: Run migrates a whole installation before it
// listens, the same boot that answers in a couple of seconds alone costs an order of
// magnitude more when every database package in the repository migrates into one
// Postgres at once, and a bound sized for the quiet case reports a working machine as
// a broken application. slowBoot is when a green wait starts saying how long it took.
func waitServing(t *testing.T, addr string, listening <-chan struct{}, stopped <-chan struct{}, runErr *error) {
	t.Helper()
	started := time.Now()
	deadline := started.Add(bootWait)
	timeout := time.After(bootWait)
	gone := func() {
		t.Fatalf("the application stopped before it served at %s: %v", addr, *runErr)
	}
	select {
	case <-listening:
	case <-stopped:
		gone()
	case <-timeout:
		t.Fatalf("the application said nothing about serving at %s within %s", addr, bootWait)
	}
	// The kernel writes that sentence one statement before the listener reports
	// whether the address was this process to take, and whatever else holds the
	// address would answer a request — so the sentence alone is not yet proof. What
	// is proof is the pair of them holding together for servingSettle: this
	// application still running, and the address still holding. A bind that never
	// succeeded arrives here as itself, which is a port and an error, rather than as
	// somebody else's 404s.
	//
	// The window opens at the first answer and closes servingSettle later, measured
	// from that answer and not from every one. Measured from every one, an address
	// that keeps answering reopens the window it had just closed, and the wait never
	// returns: it sat out bootWait and reported an application that had been served
	// for five minutes as one that never came up.
	settled := time.Time{}
	last := "nothing has answered yet"
	for time.Now().Before(deadline) {
		select {
		case <-stopped:
			gone()
		default:
		}
		got, why := healthAt(addr)
		last = why
		switch got {
		case probeNobody:
			// Nobody is home, which is what it looks like while the application is
			// still migrating. The window starts again from an answer yet to come.
			settled = time.Time{}
		case probeStranger:
			select {
			case <-stopped:
				gone()
			case <-time.After(servingSettle):
				t.Fatalf("the port %s belongs to something that is not this application: %s", addr, why)
			}
		case probeOwn, probeHeld:
			// A 200 is this application's own liveness route. An address that takes the
			// connection and says nothing over it proves only that somebody holds it —
			// and this case's own composition can be that somebody only while its bind
			// is unwritten, because a bind it lost is a Run that has already come back
			// with its error, which is read at the top of every turn of this loop.
			if settled.IsZero() {
				settled = time.Now().Add(servingSettle)
			}
			if !time.Now().Before(settled) {
				if took := time.Since(started); took > slowBoot {
					t.Logf("answered GET /health after %s: this machine was busy", took.Round(time.Millisecond))
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the application at %s never answered GET /health in %s: %s", addr,
		time.Since(started).Round(time.Second), last)
}

// bootWait is how long a fixture waits for an application it started to answer.
// Run migrates a whole installation before it listens, and the same boot that
// answers in a couple of seconds when one package runs costs an order of
// magnitude more when every database package in the repository migrates into one
// Postgres at once. A bound sized for the quiet case reports a working machine as
// a broken application, so it is sized for the loaded one and stated here rather
// than left as a literal inside the loop; slowBoot is when a green wait starts
// saying how long it took, which is what the next red run reads.
const (
	// Five minutes, the same room as kit/app's bootWait and for the same measured reason: a migration
	// of the whole installation crossed 120 s on 2026-10-06 with eight of this program's suites
	// running beside it, and a boot that is only slow on somebody else's machine is not a fault here.
	bootWait = 5 * time.Minute
	slowBoot = bootWait / 10
)

// The band freeAddr picks from. The kernel allocates nothing below
// ip_local_port_range — an :0 bind and the source port of an outbound
// connection both come out of that range — and 20000 keeps the choice clear of
// the well-known services a development host runs. What is left is ports some
// program named on purpose, which is the point: the ceiling is 30000 rather than
// the kernel's floor because a preview proxy publishing a container answered this
// suite at 30082 on 2026-10-01, and it names ports from 30000 up.
const (
	firstChosenPort = 20000
	lastChosenPort  = 29999
	chosenPortTries = 24
)

// chosenPorts remembers what this test binary has already served at, so that two
// of its cases never stand on one port. The transport every case shares pools
// connections by address, and a case that reuses a port its predecessor served
// inherits a connection to a process that has gone away.
var (
	chosenPortsMu sync.Mutex
	chosenPorts   = map[int]bool{}
)

// servingProbe is the shape one bounded question at an address took. The four are
// the four a wait can act on: whether anything is home, whether the address was
// taken without an answer, and whether what answered answered for itself.
type servingProbe int

const (
	probeNobody   servingProbe = iota // nothing took a connection: this composition has not bound yet
	probeHeld                         // a connection was taken and nothing answered over it
	probeStranger                     // an answer that is not this application's own liveness
	probeOwn                          // GET /health answered 200 at this address
)

// probeBound bounds one question at the address. An address can take a connection and
// answer nothing over it — a bare listener with no server behind it, a process parked
// behind its own queue — and a question with no deadline stops being a question and
// becomes a hang that wears a test's name.
const probeBound = time.Second

// probeClient asks the liveness route under that bound. http.Get's own client has none.
var probeClient = &http.Client{Timeout: probeBound}

// healthAt asks GET /health of addr once, within probeBound, and says which shape the
// answer took, with the sentence a failure should print beside it. It never blocks
// past probeBound: the wait that asks has a bound of its own to spend and a running
// application to watch while it spends it.
func healthAt(addr string) (servingProbe, string) {
	conn, err := net.DialTimeout("tcp", addr, probeBound)
	if err != nil {
		return probeNobody, err.Error()
	}
	_ = conn.Close()
	res, err := probeClient.Get("http://" + addr + "/health")
	if err != nil {
		// Somebody holds the address and said nothing over it. Whose it is does not
		// become knowable by waiting longer, so the caller is pointed back at the
		// application it started rather than at the socket.
		return probeHeld, fmt.Sprintf("%s took the connection and answered nothing over it (%v)", addr, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	why := fmt.Sprintf("GET /health answered %d, Server %q, body %q",
		res.StatusCode, res.Header.Get("Server"), firstLine(string(raw)))
	if res.StatusCode == http.StatusOK {
		return probeOwn, why
	}
	return probeStranger, why
}

// servingHandler watches one application's log for the sentence the kernel writes
// when it starts serving, and closes ready the first time that sentence names this
// address. It is the only way a test can tell its own application's listener from
// somebody else's on the same port, so it is deliberately not filtered by the level
// the caller chose — the caller's handler still decides what its own logger keeps.
// If the kernel ever stops writing the sentence, waitServing times out and says so;
// TestTheKernelStillSaysWhereItIsListening reddens at the change instead.
type servingHandler struct {
	addr  string
	inner slog.Handler
	ready chan struct{}
	once  sync.Once
}

func (h *servingHandler) Enabled(ctx context.Context, level slog.Level) bool { return true }

func (h *servingHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Message == "app: listening" && namedAddress(record) == h.addr {
		h.once.Do(func() { close(h.ready) })
	}
	if h.inner.Enabled(ctx, record.Level) {
		return h.inner.Handle(ctx, record)
	}
	return nil
}

// namedAddress is the address a "listening" sentence names — the kernel writes it
// as an attribute beside the sentence, and this asks for it rather than matching on
// the sentence alone, so a second application serving somewhere else on the host
// cannot close this case's wait.
func namedAddress(record slog.Record) string {
	var addr string
	record.Attrs(func(a slog.Attr) bool {
		if a.Key != "addr" {
			return true
		}
		addr = a.Value.String()
		return false
	})
	return addr
}

func (h *servingHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }

func (h *servingHandler) WithGroup(name string) slog.Handler { return h }

// freeAddr picks a port the kernel cannot hand to somebody else while this test is
// on its way to binding it, from the band stated above.
//
// The port is claimed (see claimPort) from here until the case ends, and the socket
// that proves the port free is still released, because the application under test has
// to bind the number itself. The claim is what carries the port across that window:
// the migration between this return and that bind is seconds alone and minutes with
// every database package on this host migrating into one Postgres, and the map below
// remembers only what this one process served. A run whose claim the kernel refused
// takes the next port; a claim this process cannot take at all ends the case, because
// a choice that went on without one would be sharing ports and saying nothing about it.
func freeAddr(t *testing.T) string {
	t.Helper()
	low := min(ephemeralLow()-1, lastChosenPort)
	first := rand.IntN(low - firstChosenPort + 1)
	chosenPortsMu.Lock()
	defer chosenPortsMu.Unlock()
	claimedElsewhere := 0
	for i := range chosenPortTries {
		port := firstChosenPort + (first+i)%(low-firstChosenPort+1)
		if chosenPorts[port] {
			continue
		}
		claim, err := claimPort(port)
		if err != nil {
			if !errors.Is(err, errPortClaimHeld) {
				t.Fatalf("claim port %d for this case: %v", port, err)
			}
			claimedElsewhere++
			continue
		}
		l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			claim.release()
			continue
		}
		addr := l.Addr().String()
		if !loopbackReaches(l) {
			_ = l.Close()
			claim.release()
			continue
		}
		_ = l.Close()
		chosenPorts[port] = true
		// Registered before start's stopper, so this runs after it: the case's cleanups
		// are last-in, first-out, and the port is never open to the next run while this
		// one is still serving on it.
		t.Cleanup(claim.release)
		return addr
	}
	t.Fatalf("every port between %d and %d is held by something else, %d of them claimed by another test process",
		firstChosenPort, low, claimedElsewhere)
	return ""
}

// loopbackReaches reports whether a connection to l arrives at l. The dial is
// what a case will do; the accept is what this process has to be able to get.
func loopbackReaches(l net.Listener) bool {
	accepted := make(chan bool, 1)
	go func() {
		c, err := l.Accept()
		if err == nil {
			_ = c.Close()
		}
		accepted <- err == nil
	}()
	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		return false
	}
	_ = c.Close()
	select {
	case ok := <-accepted:
		return ok
	case <-time.After(time.Second):
		return false
	}
}

// ephemeralLow is the first port the kernel allocates for a socket nobody named.
func ephemeralLow() int {
	const fallback = 32768 // Linux's own default, and the answer without the file
	body, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range")
	if err != nil {
		return fallback
	}
	low, _, _ := strings.Cut(strings.TrimSpace(string(body)), "\t")
	n, err := strconv.Atoi(low)
	if err != nil || n <= firstChosenPort {
		return fallback
	}
	return n
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
// this application's backends and nobody else's.
//
// It asks the age of the oldest open transaction of an installation started as the
// serving half, because that is the only installation this question has an answer
// for. The worker half runs the outbox relay, the outbox purge and every module's
// periodic jobs over this same pool, and a transaction of that work stays open for
// as long as its goroutine keeps a CPU: on a host carrying several suites at once
// it is seconds old, and no bound separates it from one a client pinned. On role
// web nothing but a request opens a transaction, so a transaction still open and
// old at the last sample is the trickling request's own.
func TestASlowUploadIsCutOffAndHoldsNoTransaction(t *testing.T) {
	const timeout = 3 * time.Second
	path, cfg := configure(t)
	install(t, path)
	// The only thing this test changes about the reference configuration, and
	// the key exists so that a deployment whose uploads are slower can.
	cfg.Server.ReadTimeout = timeout

	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{
		Tenants: c.testTenants(), Authorize: c.testAuth(), Entitle: c.plans, Authenticate: c.testAuth().Authenticate,
		Role: app.Web, Transport: memory.New(), Log: quiet(),
	})
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// The owner connection this installation was migrated with. It is taken
	// from the configuration rather than from dbtest a second time, because
	// asking dbtest for the URLs again would give this test a fresh schema and
	// take the tables out from under the application that is serving.
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	// longest answers the age of the oldest open transaction of this installation and
	// names the backend holding it. The name belongs in the refusal: a bare number says
	// the claim failed and nothing about which pool to go and look at, and the reader is
	// left to work out by hand what this one line already knows.
	longest := func() (float64, string) {
		var age float64
		var backend string
		const q = `SELECT EXTRACT(EPOCH FROM now() - xact_start),
				coalesce(usename::text, '?') || ' at pid ' || pid::text || ': '
				|| coalesce(left(regexp_replace(query, '\s+', ' ', 'g'), 120), '(no statement seen)')
			FROM pg_stat_activity
			WHERE state = 'idle in transaction' AND application_name = current_setting('search_path')
			ORDER BY xact_start ASC NULLS LAST LIMIT 1`
		err := owner.QueryRowContext(t.Context(), q).Scan(&age, &backend)
		if err == sql.ErrNoRows {
			return 0, ""
		}
		if err != nil {
			t.Errorf("read pg_stat_activity: %v", err)
			return 0, ""
		}
		return age, backend
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
	oldestBackend := ""
	for time.Since(start) < timeout*3/4 {
		select {
		case got := <-done:
			t.Fatalf("upload ended before the read deadline: status=%d error=%v after %s", got.code, got.err, time.Since(start))
		default:
		}
		if age, backend := longest(); age > oldest {
			oldest, oldestBackend = age, backend
		}
		samples++
		time.Sleep(20 * time.Millisecond)
	}
	if samples < 10 {
		t.Fatalf("only %d samples were taken while the body trickled", samples)
	}
	// The bound is half the read timeout, and it is arithmetic rather than taste. A
	// transaction the upload pinned opens when the request arrives and is still open at
	// the last sample, so it is at least timeout*3/4 minus one sample interval old —
	// 2.2s here. Nothing on this role opens a transaction while a body trickles and no
	// other request is in flight, so the only way to be older than the bound is to be
	// that transaction. timeout/2 refuses it with a second of room on both sides.
	if limit := timeout.Seconds() / 2; oldest > limit {
		t.Errorf("a transaction of this installation was %.2fs old while the body trickled (bound %.2fs), held by %s; the bytes are stored before the transaction is opened, so a slow client has nothing to hold",
			oldest, limit, oldestBackend)
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
	var top int64
	t.Helper()
	all := fstest.MapFS{}
	// An installation from before the split applied the files that existed then,
	// which is every version up to the one the foundation held when each module
	// took its own SQL: 000026_module_schema is that move and 000030_tenant_oidc
	// the last file numbered up to it. The split moved those files to their
	// modules, it did not add any. A file above the boundary postdates the release,
	// so the ledger must not claim it was applied — there would be no row to
	// re-own and kit/db would be right to call an applied file that no release
	// ships a contradiction
	// (migrate.go, "was applied but is missing from this release"). This is what
	// lets a module ship a version above the kernel's highest without rewriting
	// this fixture every time it does: a file that postdates the split lands here
	// as a new row, applies normally in the upgrade, and is checked as new below.
	// The direction the rule cuts is the reason a kernel file above somebody
	// else's is not free, and it is the reason the ceiling is a fact the release
	// states rather than the foundation's highest file: a source's Adopts list is
	// exactly the versions the previous owner applied and this one now ships
	// (docs/adr/0011), so the ceiling is the highest version any owner adopts back,
	// and modules/auth's 31, 32 and 33 are inside the old ledger because auth names
	// them. Had the ceiling been the foundation's own highest file, this delivery's
	// migrations/000043_tenant_app would have pushed it to 43, dragged
	// modules/audit's 35 to 37, modules/change's 38, modules/site's 39 and
	// modules/file's 40 inside the pre-split ledger — where no adoption exists to
	// re-own them — and kit/db would be right to call an applied file that no
	// release ships a contradiction. A file above that ceiling applies in the
	// upgrade below and is counted as a new row there, which is what lets any owner
	// ship any version above it without rewriting this fixture.
	var preSplitTop int64
	for _, source := range sources {
		for _, adoption := range source.Adopts {
			if adoption.Owner != "platformkit" {
				continue
			}
			for _, version := range adoption.Versions {
				if version > preSplitTop {
					preSplitTop = version
				}
			}
		}
	}
	for _, source := range sources {
		// The foundation's own files were all in the release; a module's file was
		// only in it if the module adopts that version back, which is the same
		// statement in the other direction.
		adopted := map[int64]bool{}
		for _, adoption := range source.Adopts {
			if adoption.Owner == "platformkit" {
				for _, version := range adoption.Versions {
					adopted[version] = true
				}
			}
		}
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
			digits, _, _ := strings.Cut(entry.Name(), "_")
			version, err := strconv.ParseInt(digits, 10, 64)
			if err != nil || version > preSplitTop {
				continue
			}
			if source.Owner != "platformkit" && !adopted[version] {
				continue
			}
			if version > top {
				top = version
			}
			all[entry.Name()] = &fstest.MapFile{Data: body}
		}
	}
	// Every file this fixture holds predates the runner's guard rules, and it says
	// so one past the highest version it collected rather than naming a number:
	// the flattening puts each owner's SQL at its own version under one owner, so
	// no single owner's floor is the right floor for the mixture, and the true
	// statement is that the rules arrived after all of it.
	return db.MigrationSource{Owner: "platformkit", Files: all, RulesFrom: top + 1}
}

// TestAnInstallationFromBeforeModulesOwnedTheirSQLUpgradesInPlace is the
// upgrade half of the migration move, and the half a fresh database cannot
// check: an existing installation's ledger says "platformkit" applied all
// thirty-three files at or below the adopted ceiling, and the release that splits them must neither refuse the
// files the foundation no longer ships nor run the module files again.
//
// Success alone proves the SQL did not re-run — a second 000004_task.up.sql
// would fail on CREATE TABLE tasks — and the applied_at comparison proves the
// rows were re-owned rather than replaced.
// fileKey is one shipped migration's place in the ledger: the owner that ships it
// and the version it names, which is exactly schema_migrations' PRIMARY KEY. It is
// the pair rather than the version alone because the composition now holds two
// owners at one version — modules/auth's 000035_passkeys beside modules/audit's
// 000035_audit_context — and a count keyed on the version folds those two files
// into one key, so a release that lost either row would still match the file count.
// The query below renders the same pair in SQL.
func fileKey(owner string, version int64) string {
	return owner + "/" + strconv.FormatInt(version, 10)
}

func TestAnInstallationFromBeforeModulesOwnedTheirSQLUpgradesInPlace(t *testing.T) {
	path, cfg := configureFresh(t)
	sources := app.MigrationSources(compose(cfg).modules)
	// The patient budget, not the deployment's: this case is about a ledger whose rows
	// change owner, and the legacy layout it installs carries the same concurrent build
	// every other fixture here meets. What it refuses on the budget's own terms is
	// kit/db's answer to a contended file, pinned in kit/db, not this product's upgrade.
	patient := fixtureLockTimeout
	if err := db.MigrateWith(t.Context(), cfg.Database.MigrateURL,
		db.MigrationBudget{LockTimeout: &patient}, legacyLayout(t, sources)); err != nil {
		t.Fatalf("the release before this one: %v", err)
	}
	admin := dbtest.Open(t, cfg.Database.MigrateURL)
	before := map[string]string{}
	ledger := func(into map[string]string, query string) {
		t.Helper()
		rows, err := admin.QueryContext(t.Context(), query)
		if err != nil {
			t.Fatalf("read the ledger: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var file string
			var value string
			if err := rows.Scan(&file, &value); err != nil {
				t.Fatalf("read the ledger: %v", err)
			}
			into[file] = value
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read the ledger: %v", err)
		}
	}
	ledger(before, "SELECT version::text, applied_at::text FROM schema_migrations")
	// The number is the point of the assertion: an upgrade fixture that silently
	// stopped counting a migration would pass while upgrading a real installation
	// past a file it should have applied, so the files the release holds have to
	// arrive here and say so.
	//
	// 24 became 25 when modules/user/000025 added the handle column, 25 became 26
	// when the kernel added 000026_module_schema, 26 became 27 when
	// modules/notification/000027 added the delivery ledger, 27 became 28 when the
	// kernel added 000028_outbox_trace, which gives the outbox the trace columns the
	// CloudEvents envelope carries, 28 became 29 when the kernel added
	// 000029_tenant_locale, and 29 became 30 when the kernel added
	// 000030_tenant_oidc, the per-tenant issuer columns on `tenants`, which is
	// numbered past both 29 and the highest file any module shipped until then.
	// The 29 in that chain is 29 and not the 28 it was written as, for
	// the same reason 27 moved to 28 above it: this fixture flattens every owner's
	// files under one owner, so two files at one version are one INSERT past the
	// ledger's PRIMARY KEY (owner, version) — and kit/db refuses a repeated version
	// in a source before that (migration_files.go, "invalid or repeated version").
	// Every version was one file across the composition, kernel and modules alike,
	// until main took modules/auth's passkeys at 35 under modules/audit's
	// 000035_audit_context: two owners naming one version, which kit/db allows — it
	// refuses a repeated version inside one source, and the ledger's PRIMARY KEY is
	// (owner, version) — and which is why the counts below ask the ledger for owner
	// and version together, so that two files at one version are two rows to count.
	// A new file continues past the highest number anywhere in the composition,
	// whichever owner ships it: modules/file's retention hold and erasure proof is
	// 000040 for that reason, and not the 30 it was written as when no owner held
	// it — the kernel then shipped 000030_tenant_oidc, and this fixture would have
	// flattened two files at one version into one ledger row. The kernel's own
	// 000041_outbox_baggage is that event one more time: it was written as 29, held
	// 31 and then 36 on its own branch, and 000041 is the first number no other owner
	// had taken by the time that branch reached main. This branch's
	// 000043_tenant_app is that event twice over: it numbered itself past that 41
	// rather than onto it, and then past modules/content's 000042, which main took
	// while this branch was still open — the same reason it took itself past 31, 32
	// and 33, which modules/auth took while it was in review.
	//
	// The same flattening is why modules/file's retention file is 000040 here and
	// not the 000034 it was merged as: the kernel's own 000034 moves preSplitTop
	// to 34, so a second 34 would arrive in this ledger under the same name, and
	// kit/db refuses a repeated version before the ledger ever sees it
	// (migration_files.go, "invalid or repeated version"). One version, one row.
	//
	// 33 stays 33 for those files and for every file above the ceiling, because the
	// ceiling is the highest version an owner adopts back rather than the
	// foundation's highest file — see legacyLayout for why a derivation off the
	// kernel's own top stopped working once the modules numbered past it. A file
	// above the ceiling is not absent from this test: it is the new row the upgrade
	// below has to produce, and the two counts below compare the ledger against
	// every file the release ships.
	//
	// The whole-tree census is deliberately absent: the file count this fixture
	// ships moves with every delivery that adds a migration, and a number quoted here
	// is a number the next merge silently falsifies. `find migrations modules -name
	// '*.up.sql' | wc -l` answers it, and the two counts that matter are asserted
	// below rather than restated here — the old layout's ledger against the files at
	// or beneath the adopted ceiling, and the upgraded ledger against every file the
	// release ships. What the census used to enumerate is the part worth keeping, and
	// it is a rule rather than a list: every version more than one owner holds sits
	// above the adopted ceiling, so the flattened fixture below never meets one. The
	// repeats are worth naming for what they are — 35 is modules/audit's audit_context
	// beside modules/auth's passkeys, 44 is modules/file's image dimensions beside
	// modules/notification's mail ledger and modules/webhook's endpoint table, and 54
	// and 55 are this delivery's two delivery marks beside modules/file's variants and
	// unused-since, modules/notification's channel and sender tables and modules/site's
	// default accent. Which versions are repeated moves with every release; that they
	// sit above the ceiling is what the fixture enforces. Every file above the ceiling
	// continues past the highest number anywhere in the composition at the time it was
	// written, which is the rule this fixture exists to enforce — and it is a rule with
	// teeth rather than tidiness, because kit/db reads a repeated version only inside
	// one owner: the kernel's 000034_outbox_request and a modules/auth file numbered 34
	// apply side by side under two owners, and the only thing that says so is the map
	// below, which counts every shipped file under its owner and its version together,
	// exactly as schema_migrations keys them. T-0231's four are this rule's newest
	// customer: they arrived at 000044, 000045, 000046 and 000047, main took 000046 for
	// its own SAML provider columns in between, and the flattened fixture below reads
	// one version as one row whatever the owner, so they are 000058, 000059, 000060 and
	// 000061. modules/audit's 48 and 49 are the customer before them: the append-only
	// trail and its retention marks were numbered 41 and 42 while the branch was open,
	// and main took 41 for the kernel's own outbox baggage and 42 for modules/content's
	// body characters underneath it. This delivery's two marks are the same event one
	// step earlier: they stood at 000050 and 000051 while the branch was open, and the
	// first merge of main took 50, 51 and 52 for the kernel's demo marker, seed keys and
	// outbox attribution and 53 for modules/audit's copy into the trail underneath
	// them, so the verification mark went to 000054 and the password-reset mark, the
	// same column one table further along, to 000055. Both numbers were already held by
	// another owner, which kit/db allows because its ledger key is (owner, version),
	// and which the map below still counts as two rows because it keys that pair — but
	// the modules/auth test reads the file by name, so a renumber moves that name too.
	//
	// modules/content's body-character file is 000042 and not the 000035 it was first
	// merged as, for the reason this paragraph has now recorded six times: this fixture
	// flattens every owner's files under one owner, and every number from 35 to 45 was
	// taken on main while another branch was still open — 000035 is modules/audit's
	// audit_context, 000041 is the kernel's own outbox baggage, and 000044 and 000045 are
	// modules/file's image dimensions and file uses. One version, one row. This
	// delivery's own four arrived as 000035, 000036, 000037 and modules/audit's 000038,
	// then as 000044, 000045, 000046 and modules/audit's 000047, then as 000046, 000047,
	// 000048 and modules/audit's 000049, and are 000050 (the tenant's demo marker),
	// 000051 (the seed's provenance keys), 000052 (the attribution columns beside each
	// outbox row) and modules/audit's 000053 (those four copied into the trail) for
	//
	// This delivery's own four continue past that: modules/notification's channel
	// preferences and quiet window, verified sender, sent-once index and sender
	// revision are 000054, 000055, 000056 and 000057, numbered past modules/audit's
	// 000053 for the same reason. They sit above the adopted ceiling, so they are
	// absent from the old ledger and are counted as new rows by the upgrade below,
	// and modules/notification's Adopts names only 000011 and 000027 — the two
	// files its owner applied under "platformkit" before the split.
	// that one reason: while this branch was open main took 000046 for its own SAML
	// provider columns, 000047 for modules/auth's replay table and 000048 and 000049
	// for modules/audit's append-only trail and its retention marks — and two files at
	// one version under the one owner `audit` is a refusal in kit/db before it is ever
	// a ledger row. modules/scim's group, mapping and person tables arrived on their
	// branch as 000044, then as 000054, and are 000064 here: main took 000044 for
	// modules/file's image dimensions while that branch was open, then took 000054 twice
	// more for modules/file's variants and modules/site's default accent, then 000058 to
	// 000062 for its own ledger lock, refusal, placement and idempotency files and
	// 000063 for modules/file's media alt text, and 000064 continues past 000063, the
	// highest number any owner holds at the commit this merge lands.
	if len(before) != 33 {
		t.Fatalf("the old layout applied %d files, want 33", len(before))
	}

	// The new release, through the path a person runs: bootstrap migrates with
	// the composed sources before it writes the first tenant.
	install(t, path)
	// And the rest of the convergence, which is the same path: 000061 sits behind 000060,
	// the release's placement window, and kit/db's planOwner bounds a phase=data file only
	// when nothing of its owner is pending behind it — so this boot stops at the window and
	// the two steps that finish a release take it: the drain the worker's first tick runs,
	// then the migration again for the file that waited behind it. That trio is what
	// `pkit migrate --drain` runs (apps/platformkit/migrate.go) and what
	// scripts/rehearse_migrations.sh measures. The ledger below is the ledger of the
	// converged installation, and the count it must reach is every file the release ships.
	if err := app.Drain(t.Context(), cfg, compose(cfg).modules); err != nil {
		t.Fatalf("the drain that finishes the upgrade: %v", err)
	}
	if err := app.Migrate(t.Context(), cfg, compose(cfg).modules); err != nil {
		t.Fatalf("the migration behind the drain: %v", err)
	}

	owners := map[string]string{}
	ledger(owners, "SELECT owner || '/' || version::text, owner FROM schema_migrations")
	after := map[string]string{}
	ledger(after, "SELECT version::text, applied_at::text FROM schema_migrations")
	// The upgrade re-owns the old rows and applies the files that postdate them,
	// so the ledger grows by exactly the new files and by nothing else: a fixture
	// that re-ran an old file would be caught below by applied_at, and one that
	// lost a row would be caught here.

	for version, when := range before {
		if after[version] != when {
			t.Errorf("version %s was applied again: %s became %s", version, when, after[version])
		}
	}
	// Each file now reads under the owner that ships it — and every file this
	// release ships is in the ledger under the owner that ships it, which is where the
	// files above the adopted ceiling have to be accounted for. Both maps below are
	// keyed by owner and version together, exactly as schema_migrations is, so a
	// version two owners hold is two rows in each of them and the comparison is
	// between the ledger and the sources the composition names, never against a count
	// carried in this comment.
	want := map[string]string{}
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
			want[fileKey(source.Owner, version)] = source.Owner
		}
	}
	if len(owners) != len(want) {
		t.Fatalf("the upgrade left %d applied files, want the %d this release ships", len(owners), len(want))
	}
	for file, owner := range want {
		if owners[file] != owner {
			t.Errorf("file %s reads as %q, want %q", file, owners[file], owner)
		}
	}
	// And the application built on that schema serves.
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{
		Tenants: c.testTenants(), Authorize: c.testAuth(), Entitle: c.plans, Authenticate: c.testAuth().Authenticate,
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
		Tenants: c.testTenants(), Authorize: c.testAuth(), Entitle: c.plans, Authenticate: c.testAuth().Authenticate,
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
		Tenants: c.testTenants(), Authorize: c.testAuth(), Entitle: c.plans, Authenticate: c.testAuth().Authenticate,
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
