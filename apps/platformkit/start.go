package main

// start.go is the one command: from nothing to a running application.
//
//	go run github.com/septagon-oss/platformkit/apps/platformkit@main start
//
// It runs its own PostgreSQL — downloaded once into the user's cache, its data
// under ./data — creates the application role, migrates, bootstraps the first
// tenant and administrator when there is none, and serves on :8080. Nothing has
// to be installed but Go, and nothing has to be written down: the configuration
// is the development one config.example.yaml documents, with the database's
// port filled in.
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
	"strings"
	"syscall"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// The first tenant. The host is what the browser types, and .localhost
// resolves to the machine without a hosts-file entry.
const (
	startTenant = "platformkit"
	startHost   = "platformkit.localhost"
	startAdmin  = "admin@platformkit.localhost"
)

// postgresInit is the SQL that makes the application role: NOSUPERUSER and
// NOBYPASSRLS, so row-level security constrains it. It is the same file the
// Compose stack and CI run through psql; there is one of it.
//
//go:embed postgres-init.sql
var postgresInit string

func startApp(args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	data := fs.String("data", "data", "Directory for the database and uploaded files")
	addr := fs.String("addr", ":8080", "Address to listen on")
	if err := fs.Parse(args); err != nil {
		return err
	}
	logger("info")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	port, err := freePort()
	if err != nil {
		return err
	}
	cfg := startConfig(*data, *addr, port)
	pg, err := startPostgres(*data, port)
	if err != nil {
		return err
	}
	defer func() {
		if err := pg.Stop(); err != nil {
			fmt.Fprintln(os.Stderr, "platformkit: stop postgres:", err)
		}
	}()
	if err := ensureRole(ctx, cfg.Database.MigrateURL); err != nil {
		return err
	}

	c := compose(cfg)
	password, err := generatePassword()
	if err != nil {
		return err
	}
	err = app.Bootstrap(ctx, cfg, c.modules, func(ctx context.Context, tx db.Tx[db.System]) error {
		t, err := tenant.Bootstrap(ctx, tx, c.tenants, tenantcontracts.NewTenant{
			Slug: startTenant, Name: "PlatformKit", Host: startHost,
		})
		if err != nil {
			return err
		}
		_, err = c.users.Provision(ctx, tx, t.ID, startAdmin, "", password, []string{authcontracts.RoleAdmin})
		return err
	})
	switch {
	case err == nil:
		fmt.Fprintf(os.Stderr, "\n  tenant %s at %s, administrator %s\n  password: %s\n  It is not stored and will not be shown again.\n\n",
			startTenant, startHost, startAdmin, password)
	case errors.Is(err, crud.ErrConflict):
		// The second run: the tenant is there, and so is the password from
		// the first one. Migrations ran on the way past, which is the point.
		fmt.Fprintf(os.Stderr, "  tenant %s exists; sign in as %s with the password from the first run\n", startTenant, startAdmin)
	default:
		return err
	}

	a, err := app.New(ctx, cfg, c.modules, app.Options{
		Tenants:      c.tenants,
		Authorize:    c.auth,
		Authenticate: c.auth.Authenticate,
		Role:         app.All,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  site   http://%s/\n  admin  http://%s/admin/login\n  api    http://%s/docs\n\n", cfg.Server.PublicHost, cfg.Server.PublicHost, cfg.Server.PublicHost)
	return a.Run(ctx)
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
	}
}

// startPostgres runs PostgreSQL 16 — the version the Compose stack runs — with
// its binaries cached per user and its data under the data directory, so a
// second start finds both. Its own logging goes nowhere: a database that starts
// has nothing to say, and one that does not is an error this returns.
func startPostgres(data string, port int) (*embeddedpostgres.EmbeddedPostgres, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("start: no cache directory for the database binaries: %w", err)
	}
	cache = filepath.Join(cache, "platformkit", "postgres")
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Port(uint32(port)).
		Username("postgres").Password("platformkit").Database("platformkit").
		CachePath(filepath.Join(cache, "archive")).
		RuntimePath(filepath.Join(cache, "runtime")).
		DataPath(filepath.Join(data, "postgres")).
		Logger(io.Discard).
		StartTimeout(90 * time.Second))
	if err := pg.Start(); err != nil {
		return nil, fmt.Errorf("start: postgres: %w", err)
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
