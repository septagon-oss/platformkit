// Package main — cli.go owns the cobra command tree for the front door.
//
// Design rules:
//   - `platformkit` with no subcommand serves, so every published quickstart
//     (`go run github.com/septagon-oss/platformkit@latest`) keeps working.
//   - Configuration precedence, lowest to highest: built-in defaults →
//     config.yaml → environment variables → flags.
//   - Introspection commands (modules, openapi) compose the real application
//     against a throwaway in-memory database so they never touch ./pk.db or a
//     configured deployment database.
//   - Machine consumers (scripts, LLM agents) get --json where output is data.
//
// Implements: REQ-016.
// Per: ADR-0017, ADR-0029.
// Discipline: C-14.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"

	"github.com/septagon-oss/pk-apps/pkg/starterapp"
)

// serveFlags are the command-line overrides for the serve path. The zero
// value means "no override" for every field.
type serveFlags struct {
	config        string
	addr          string
	port          int
	env           string
	dbDSN         string
	adminEmail    string
	adminPassword string
}

func addServeFlags(cmd *cobra.Command, f *serveFlags) {
	fl := cmd.Flags()
	fl.StringVarP(&f.config, "config", "c", "", "path to config.yaml (overrides PK_CONFIG and ./config.yaml discovery)")
	fl.StringVar(&f.addr, "addr", "", "listen address host:port (required for non-loopback exposure)")
	fl.IntVarP(&f.port, "port", "p", 0, "loopback port shorthand for --addr 127.0.0.1:<port>")
	fl.StringVar(&f.env, "env", "", "environment override: development or production")
	fl.StringVar(&f.dbDSN, "db-dsn", "", "database DSN override")
	fl.StringVar(&f.adminEmail, "admin-email", "", "seed administrator email override")
	fl.StringVar(&f.adminPassword, "admin-password", "", "seed administrator password override (prefer PK_ADMIN_PASSWORD so the secret stays out of the process list)")
}

// resolveConfig builds the effective Config: defaults, then the optional
// config file, then environment variables, then flags.
func resolveConfig(lookup func(string) string, f serveFlags) (*starterapp.Config, error) {
	path := f.config
	if path != "" {
		// An explicit --config that does not exist is an operator error and
		// must say so, not silently fail closed into "password required".
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("--config %s: %w", path, err)
		}
	} else {
		path = configPath(lookup)
	}

	cfg := starterapp.DefaultConfig()
	if path != "" {
		loaded, err := starterapp.LoadConfig(path)
		if err != nil {
			return nil, err
		}
		cfg = loaded
	}

	starterapp.ApplyAddressOverrides(cfg, lookup)
	if v := strings.TrimSpace(lookup("PK_ADMIN_EMAIL")); v != "" {
		cfg.Seed.AdminEmail = v
	}
	if v := strings.TrimSpace(lookup("PK_ADMIN_PASSWORD")); v != "" {
		cfg.Seed.AdminPassword = v
	}

	switch {
	case f.addr != "":
		cfg.HTTP.Addr = f.addr
	case f.port != 0:
		cfg.HTTP.Addr = fmt.Sprintf("127.0.0.1:%d", f.port)
	}
	if f.env != "" {
		cfg.Environment = f.env
	}
	if f.dbDSN != "" {
		cfg.Database.DSN = f.dbDSN
	}
	if f.adminEmail != "" {
		cfg.Seed.AdminEmail = f.adminEmail
	}
	if f.adminPassword != "" {
		cfg.Seed.AdminPassword = f.adminPassword
	}
	return cfg, nil
}

func runWith(ctx context.Context, lookup func(string) string, start starterRunner, f serveFlags) error {
	cfg, err := resolveConfig(lookup, f)
	if err != nil {
		return err
	}
	return start(ctx, cfg)
}

const rootLong = `PlatformKit OSS — a batteries-included nine-module SaaS monolith.

Running with no subcommand starts the server. Zero-config boots a
loopback-only development instance with a seeded local tenant and
administrator; a config.yaml next to the process (or named by PK_CONFIG)
switches to fail-closed production configuration.

Configuration precedence, lowest to highest:
  built-in defaults → config.yaml → environment variables → flags

Environment variables:
  PK_CONFIG          path to config.yaml (default: ./config.yaml if present)
  PK_HTTP_ADDR       explicit listen address (required for non-loopback)
  PORT               loopback port shorthand (127.0.0.1:$PORT)
  PK_ADMIN_EMAIL     seed administrator email
  PK_ADMIN_PASSWORD  seed administrator password (required outside development)

Examples:
  go run github.com/septagon-oss/platformkit@latest
  go run github.com/septagon-oss/platformkit@latest --port 9090
  go run github.com/septagon-oss/platformkit@latest config init
  go run github.com/septagon-oss/platformkit@latest --env production --admin-password "$SECRET"
  go run github.com/septagon-oss/platformkit@latest modules --json
  go run github.com/septagon-oss/platformkit@latest openapi > openapi.json`

func newRootCmd() *cobra.Command {
	var f serveFlags
	serveRun := func(cmd *cobra.Command, _ []string) error {
		return runWith(cmd.Context(), os.Getenv, starterapp.Run, f)
	}

	root := &cobra.Command{
		Use:           "platformkit",
		Short:         "PlatformKit OSS — the nine-module starter monolith",
		Long:          rootLong,
		Args:          cobra.NoArgs,
		RunE:          serveRun,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	addServeFlags(root, &f)

	serve := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP server (same as running with no subcommand)",
		Args:  cobra.NoArgs,
		RunE:  serveRun,
	}
	addServeFlags(serve, &f)

	root.AddCommand(serve, newNewCmd(), newVersionCmd(), newModulesCmd(), newOpenAPICmd(), newConfigCmd())
	return root
}

type buildVersion struct {
	Version    string `json:"version"`
	APIVersion string `json:"api_version"`
	Go         string `json:"go"`
}

func versionInfo() buildVersion {
	v := "(devel)"
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		v = bi.Main.Version
	}
	return buildVersion{
		Version: v,
		// Single-sourced from the module set via DefaultConfig so the CLI and
		// the OpenAPI document cannot disagree.
		APIVersion: starterapp.DefaultConfig().AppVersion,
		Go:         runtime.Version(),
	}
}

func newVersionCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the release and API contract versions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := versionInfo()
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(info)
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "platformkit %s (api %s, %s)\n", info.Version, info.APIVersion, info.Go)
			return err
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

// buildEphemeralApp composes the full application against a throwaway
// in-memory database so introspection commands never create or migrate
// ./pk.db (or any configured deployment database) as a side effect.
func buildEphemeralApp(ctx context.Context) (*starterapp.App, error) {
	cfg := starterapp.DefaultConfig()
	cfg.Database.DSN = "file:platformkit-cli?mode=memory&cache=shared"
	return starterapp.BuildApp(ctx, cfg)
}

func newModulesCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "modules",
		Short: "List the composed business modules without starting the server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := buildEphemeralApp(cmd.Context())
			if err != nil {
				return err
			}
			defer app.Close()
			ids := app.AllModuleIDs()
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(ids)
			}
			for _, id := range ids {
				fmt.Fprintln(cmd.OutOrStdout(), id)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func newOpenAPICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "openapi",
		Short: "Print the OpenAPI extensions document without starting the server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := buildEphemeralApp(cmd.Context())
			if err != nil {
				return err
			}
			defer app.Close()
			mux, err := app.Mux()
			if err != nil {
				return err
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi/extensions.json", nil))
			if rec.Code != http.StatusOK {
				return fmt.Errorf("openapi endpoint returned status %d", rec.Code)
			}
			_, err = cmd.OutOrStdout().Write(rec.Body.Bytes())
			return err
		},
	}
}

// configTemplate is the commented starter config. It must always round-trip
// through starterapp.LoadConfig (the parser rejects unknown keys); the test
// suite enforces that. seed.admin_password stays commented out on purpose:
// a fresh production config must fail closed until the operator sets one.
const configTemplate = `# PlatformKit configuration.
# The presence of this file signals a real deployment: environment defaults
# to production, which REQUIRES an admin password. Provide it through the
# environment, not this file:
#
#     export PK_ADMIN_PASSWORD='a-long-random-secret'
#
# The process applies PK_ADMIN_PASSWORD after loading this file, so the secret
# stays out of your config, your git history, and your image layers. Leave
# seed.admin_password commented out.

app_name: platformkit
environment: production # development | production

http:
  addr: 127.0.0.1:8080 # set an explicit non-loopback address to expose
  read_timeout: 30s
  write_timeout: 30s
  shutdown_timeout: 30s

database:
  driver: sqlite
  dsn: "file:./pk.db?_pragma=busy_timeout(5000)&cache=shared&mode=rwc"

cache:
  provider: memory

seed:
  admin_email: admin@example.com
  # REQUIRED outside development — uncomment and set a strong secret:
  # admin_password: generate-a-long-random-secret
`

func newConfigCmd() *cobra.Command {
	cfg := &cobra.Command{
		Use:   "config",
		Short: "Configuration helpers",
	}

	var force bool
	initCmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Write a commented config.yaml template",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "config.yaml"
			if len(args) == 1 {
				path = args[0]
			}
			if !force {
				if _, err := os.Stat(path); err == nil {
					return fmt.Errorf("%s already exists (use --force to overwrite)", path)
				}
			}
			if err := os.WriteFile(path, []byte(configTemplate), 0o600); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s — set seed.admin_password before deploying\n", path)
			return nil
		},
	}
	initCmd.Flags().BoolVar(&force, "force", false, "overwrite an existing file")

	cfg.AddCommand(initCmd)
	return cfg
}
