package main

// start.go is the one command: from nothing to a running application.
//
//	go run github.com/septagon-oss/platformkit/apps/platformkit@main start
//
// It runs its own PostgreSQL — downloaded once into the user's cache, its data
// under ./data — creates the application role, migrates, bootstraps the first
// tenant and administrator when there is none, and serves on 127.0.0.1:8080:
// nothing outside this machine reaches it unless --addr says otherwise. Nothing
// has to be installed but Go and nothing has to be written down: the
// configuration is the development one config.example.yaml documents.
//
// NATS is not needed: the combined role carries events in memory, which is
// what kit/app does for it anyway. A deployment never runs this; a deployment
// has a config.yaml and a database of its own, and uses `run`.

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/admin"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// The first tenant. The host is what the browser types, and .localhost
// resolves to the machine without a hosts-file entry.
const (
	startTenant = "platformkit"
	startHost   = "platformkit.localhost"
	startAdmin  = "admin@platformkit.localhost"

	// defaultAddr names a host on purpose. A bare ":port" binds every interface
	// the machine has, and this command hands out an administrator's password to
	// whoever reads the terminal next — so the address it answers with when
	// nobody names one is the loopback, and a run that means to serve a network
	// says so in --addr. README.md states the same fact; the case in this
	// directory refuses the two disagreeing.
	defaultAddr = "127.0.0.1:8080"
)

// postgresInit is the SQL that makes the application role: NOSUPERUSER and
// NOBYPASSRLS, so row-level security constrains it. It is the same file the
// Compose stack and CI run through psql; there is one of it.
//
//go:embed postgres-init.sql
var postgresInit string

func startApp(args []string, d startDeps) error {
	d = d.withDefaults()
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	data := fs.String("data", "data", "Directory for the database and uploaded files")
	addr := fs.String("addr", defaultAddr, "Address to listen on; the default answers this machine only")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// The preflight first: the machine, the address and the two directories are
	// answered before anything is downloaded or written, so a run that cannot
	// work costs the seconds it took to find that out and leaves nothing behind.
	run, err := d.preflight(*data, *addr)
	if err != nil {
		return err
	}
	defer run.release()
	logger("info")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	port, err := freePort()
	if err != nil {
		return err
	}
	cfg := startConfig(run.data, *addr, port)
	pending := run.launch
	pending.port = port
	pg, err := d.launchDB(pending)
	if err != nil {
		return startFailure(err, run)
	}
	defer func() {
		if err := pg.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "platformkit: stop postgres: %s\n  if a server still holds %s, stop it with: %s stop -D %s\n",
				err, run.cluster(), pgCtl(run.cache), run.cluster())
		}
	}()
	if err := ensureRole(ctx, cfg.Database.MigrateURL); err != nil {
		return err
	}

	c := composeReference(cfg, pkit.Development)
	password, err := generatePassword()
	if err != nil {
		return err
	}
	err = app.Bootstrap(ctx, cfg, c.plan().Modules(), func(ctx context.Context, tx db.Tx[db.System]) error {
		return c.bootstrapIdentity(ctx, tx, tenantcontracts.NewTenant{
			Slug: startTenant, Name: "PlatformKit", Host: startHost,
		}, startAdmin, password, nil)
	})
	use := firstUse{
		reference: admin.Build(),
		data:      run.data,
		cluster:   run.cluster(),
		cache:     run.cache,
		public:    cfg.Server.PublicHost,
	}
	switch {
	case err == nil:
		// The first run: the administrator is new, so this is the only time
		// the password exists in a form a person can read.
		use.password = password
	case errors.Is(err, crud.ErrConflict):
		// The second run: the tenant is there, and so is the password from the
		// first one. Migrations ran on the way past, which is the point, and the
		// banner says as much rather than printing a password nobody can use.
	default:
		return err
	}
	use.write(os.Stderr)
	// The address goes to the application now: the preflight held it so that
	// nothing could take it on the way here, and the kernel's own bind is the
	// authority from here on.
	run.release()
	if err := c.app.Run(ctx, c.once, app.All); err != nil {
		return lostBind(err, run)
	}
	return nil
}

// lostBind is the bind the preflight won losing the race after the run handed the
// address back: the application's own bind is the authority, so its refusal is
// what a person sees. It is the occupied-address sentence restated for a run that
// had already built the installation by then — the database, the role and the
// tenant exist and stand untouched — and nothing else, because every other failure
// of Run needs the error the kernel wrote.
func lostBind(err error, run startup) error {
	if !errors.Is(err, syscall.EADDRINUSE) {
		return err
	}
	return fmt.Errorf("start: %s was free when this command checked it and is answered by something else now: %w\n"+
		"       nothing was written by this refusal: the installation under %s is complete and untouched,\n"+
		"       so stop whatever answers %s and rerun this command, which reuses it, or choose\n"+
		"       another address with --addr, for example --addr 127.0.0.1:8081",
		run.addr, err, run.data, run.addr)
}

// startFailure is the launcher's failure with the next step attached. The
// library reports what it was doing; what a person can do about it depends on
// which of its three external dependencies refused, so the sentence is chosen
// from the error rather than left to the log.
func startFailure(err error, run startup) error {
	text := err.Error()
	switch {
	case strings.Contains(text, "unable to connect"):
		return fmt.Errorf("start: the first start downloads PostgreSQL %s from %s and this machine could not reach it: %w\n"+
			"       the run stops before any migration, so no tenant was created;\n"+
			"       rerunning this command retries it; the binaries stay under %s once one has arrived",
			embeddedVersion, downloadRepository, err, run.cache)
	case strings.Contains(text, "no version found matching"):
		return fmt.Errorf("start: no PostgreSQL %s build is published for this machine: %w\n"+
			"       the run stops before any migration, so no tenant was created;\n"+
			"       this command asks %s for it; the machine it is running on is %s/%s",
			embeddedVersion, err, downloadRepository, runtime.GOOS, runtime.GOARCH)
	case strings.Contains(text, "postmaster.pid"), strings.Contains(text, "another server might be running"):
		return fmt.Errorf("start: a server this run did not start holds %s: %w\n"+
			"       nothing was deleted and no server was stopped;\n"+
			"       stop it with: %s stop -D %s, then rerun — this command never stops a server it did not start",
			run.cluster(), err, pgCtl(run.cache), run.cluster())
	default:
		return fmt.Errorf("start: postgres: %w\n"+
			"       the run stops before any migration, so no tenant was created;\n"+
			"       the launcher writes its own log under %s, which names the reason",
			err, filepath.Join(run.cache, "runtime"))
	}
}

// downloadRepository is where the embedded launcher fetches its binaries from.
// It is the library's own default, named here so a refusal can say which host a
// person is being asked to let through the firewall; this repository pins the
// version, not a digest, and signs nothing.
const downloadRepository = "https://repo1.maven.org/maven2"

// firstUse is the banner: what is running, where it writes, where to go, and
// what rerunning this command will do. Every field is read from the run, so the
// banner cannot advertise an address the run is not on.
type firstUse struct {
	reference string
	data      string
	cluster   string
	cache     string
	public    string

	// password is printed by the run that created the administrator and by no
	// other: an installation that already has one has no new secret to hand out.
	password string
}

func (f firstUse) write(w io.Writer) {
	say := func(format string, args ...any) { fmt.Fprintf(w, format+"\n", args...) }
	say("  reference %s", f.reference)
	say("  data      %s  (uploads under %s/files)", f.data, f.data)
	say("  database  %s  (binaries under %s)", f.cluster, filepath.Join(f.cache, "runtime"))
	say("  tenant    %s, administrator %s", startTenant, startAdmin)
	switch {
	case f.password != "":
		say("  password: %s", f.password)
		say("            It is not stored and will not be shown again.")
	default:
		say("            the tenant already exists: sign in with the password the first run printed.")
	}
	say("  site      http://%s/", f.public)
	say("  admin     http://%s/app/admin/login", f.public)
	say("  api       http://%s/docs", f.public)
	say("  next      sign in at the admin address, or from a shell:")
	say("            curl -c cookies.txt -H 'content-type: application/json' \\")
	say("              -d '{\"email\":\"%s\",\"password\":\"<the password above>\"}' http://%s/api/v1/auth/login", startAdmin, f.public)
	say("            curl -b cookies.txt -H 'content-type: application/json' \\")
	say("              -d '{\"title\":\"first task\"}' http://%s/api/v1/task/tasks", f.public)
	say("  stop      Ctrl-C; rerunning this command reuses %s, creates nothing new and starts no second server", f.data)
	say("")
}

// startConfig is the development configuration with the database's port in
// it. It mirrors config.example.yaml on purpose and is not read from it: that
// file is a deployment's template, and a template a program parses to run is a
// second configuration surface.
func startConfig(data, addr string, pgPort int) config.Config {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		port = strings.TrimPrefix(addr, ":")
	}
	url := func(user string) string {
		return fmt.Sprintf("postgres://%s:platformkit@127.0.0.1:%d/platformkit?sslmode=disable&connect_timeout=5", user, pgPort)
	}
	return config.Config{
		Server:   config.Server{Addr: addr, PublicHost: startHost + ":" + port, ReadTimeout: 30 * time.Second, Docs: true},
		Database: config.Database{URL: url("platformkit_app"), MigrateURL: url("postgres")},
		Log:      config.Log{Level: "info"},
		Auth:     config.Auth{OIDC: config.OIDC{RedirectPath: "/api/v1/auth/oidc/callback"}},
		Mail:     config.Mail{Port: 587},
		Audit:    config.Audit{RetentionDays: 365},
		Files:    config.Files{Dir: filepath.Join(data, "files"), MaxBytes: 25 << 20, QuotaBytes: 1 << 30},
		Webhook:  config.Webhook{SecretKey: startWebhookKey},
	}
}

// startWebhookKey is the key the start command wraps every endpoint's signing
// secret with, and the value config.example.yaml carries for `make run`: a
// test reads that file and refuses the two spellings once they disagree, because
// an installation that migrated its endpoints under one key and restarted with the
// other cannot open the secrets it already wrapped. modules/webhook refuses to be
// composed without a key and this command has no secret store in front of it, so it
// carries the laptop's own — not a secret and not meant to be one, for the reason
// the database password in the two URLs above is not.
//
// It is read from nowhere else, not even the environment kit/config hands out for
// it: a command whose whole configuration is written here would otherwise hold one
// field from outside the file. A deployment does not run this command; it has a
// config.yaml of its own, its key in PLATFORMKIT_WEBHOOK_SECRET_KEY.
const startWebhookKey = "0123456789abcdef0123456789abcdef"

// embeddedVersion is the PostgreSQL this command runs — the version the Compose
// stack runs — and the major version a data directory has to name to be reused.
const embeddedVersion = embeddedpostgres.V16

// pgCtl is the server-control binary the launcher extracted into the cache. A
// refusal prints it, so a person can stop a server this command refuses to stop.
func pgCtl(cache string) string {
	return filepath.Join(cache, "runtime", "bin", "pg_ctl")
}

// cluster is the database directory inside the data directory.
// startPostgres is the launcher: PostgreSQL with its binaries under the cache the
// preflight settled and its data under the data directory, so a second start finds
// both. Its own logging goes nowhere: a database that starts has nothing to say,
// and one that does not is the error this returns for startFailure to classify.
//
// The first start on a machine downloads the binaries for that machine, and an
// interruption leaves no partial archive behind: rerunning this command retries it.
func startPostgres(l launch) (database, error) {
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedVersion).
		Port(uint32(l.port)).
		Username("postgres").Password("platformkit").Database("platformkit").
		CachePath(filepath.Join(l.cache, "archive")).
		RuntimePath(filepath.Join(l.cache, "runtime")).
		DataPath(l.cluster()).
		Logger(io.Discard).
		StartTimeout(90 * time.Second))
	if err := pg.Start(); err != nil {
		return nil, err
	}
	return pg, nil
}

// ensureRole creates the application role on a fresh database and does nothing
// on one that has it. The statements are the embedded file's, one at a time.
func ensureRole(ctx context.Context, migrateURL string) error {
	pool, err := sql.Open("pgx", migrateURL)
	if err != nil {
		return fmt.Errorf("start: open: %w", err)
	}
	defer pool.Close()
	var n int
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM pg_roles WHERE rolname = 'platformkit_app'").Scan(&n); err != nil {
		return fmt.Errorf("start: read roles: %w", err)
	}
	if n > 0 {
		return nil
	}
	for _, statement := range statements(postgresInit) {
		if _, err := pool.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("start: %s: %w", firstLine(statement), err)
		}
	}
	return nil
}

// statements splits a SQL file into what psql would run: comment lines dropped,
// one statement per semicolon.
func statements(file string) []string {
	var kept []string
	for _, line := range strings.Split(file, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "--") {
			kept = append(kept, line)
		}
	}
	var out []string
	for _, s := range strings.Split(strings.Join(kept, "\n"), ";") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// freePort is a port nothing listens on right now, for a database only this
// process talks to.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("start: no free port: %w", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
