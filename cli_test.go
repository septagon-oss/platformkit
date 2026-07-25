// Validates: REQ-016.
// Per: ADR-0017.
// Discipline: C-14.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/septagon-oss/pk-apps/pkg/starterapp"
)

func execCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestResolveConfigFlagsBeatEnvironment(t *testing.T) {
	t.Parallel()
	lookup := func(key string) string {
		switch key {
		case "PORT":
			return "9090"
		case "PK_ADMIN_EMAIL":
			return "env@example.com"
		case "PK_ADMIN_PASSWORD":
			return "env-password-long-enough"
		}
		return ""
	}
	cfg, err := resolveConfig(lookup, serveFlags{
		port:       7070,
		env:        "production",
		adminEmail: "flag@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Addr != "127.0.0.1:7070" {
		t.Fatalf("addr = %q, want flag port to beat PORT env", cfg.HTTP.Addr)
	}
	if cfg.Seed.AdminEmail != "flag@example.com" {
		t.Fatalf("admin email = %q, want flag to beat env", cfg.Seed.AdminEmail)
	}
	if cfg.Seed.AdminPassword != "env-password-long-enough" {
		t.Fatalf("admin password = %q, want env fallback when no flag", cfg.Seed.AdminPassword)
	}
	if cfg.Environment != "production" {
		t.Fatalf("environment = %q, want flag override", cfg.Environment)
	}
}

func TestResolveConfigAddrBeatsPort(t *testing.T) {
	t.Parallel()
	cfg, err := resolveConfig(func(string) string { return "" }, serveFlags{addr: "0.0.0.0:8081", port: 9090})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Addr != "0.0.0.0:8081" {
		t.Fatalf("addr = %q, want explicit --addr to win", cfg.HTTP.Addr)
	}
}

func TestResolveConfigExplicitMissingFileErrors(t *testing.T) {
	t.Parallel()
	_, err := resolveConfig(func(string) string { return "" }, serveFlags{config: filepath.Join(t.TempDir(), "nope.yaml")})
	if err == nil {
		t.Fatal("want loud error for explicit --config pointing at a missing file")
	}
}

func TestResolveConfigFlagConfigFileLoads(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("app_name: acme\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := resolveConfig(func(string) string { return "" }, serveFlags{config: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppName != "acme" {
		t.Fatalf("AppName = %q, want value from --config file", cfg.AppName)
	}
	if cfg.Environment != "production" {
		t.Fatalf("Environment = %q, want fail-closed production for a configured deployment", cfg.Environment)
	}
}

func TestConfigInitWritesLoadableTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := execCLI(t, "config", "init", path); err != nil {
		t.Fatal(err)
	}

	// The template must round-trip through the strict parser.
	cfg, err := starterapp.LoadConfig(path)
	if err != nil {
		t.Fatalf("template does not parse: %v", err)
	}
	if cfg.Environment != "production" {
		t.Fatalf("template environment = %q, want production", cfg.Environment)
	}
	if cfg.Seed.AdminPassword != "" {
		t.Fatal("template must NOT ship an admin password — production boots must fail closed until one is set")
	}

	// Refuses to clobber, honours --force.
	if _, err := execCLI(t, "config", "init", path); err == nil {
		t.Fatal("want error when the file already exists")
	}
	if _, err := execCLI(t, "config", "init", "--force", path); err != nil {
		t.Fatalf("--force overwrite: %v", err)
	}
}

func TestVersionJSON(t *testing.T) {
	t.Parallel()
	out, err := execCLI(t, "version", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var info buildVersion
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatalf("version --json output %q: %v", out, err)
	}
	if info.APIVersion != starterapp.DefaultConfig().AppVersion {
		t.Fatalf("api version = %q, want %q", info.APIVersion, starterapp.DefaultConfig().AppVersion)
	}
}

func TestRootRejectsUnknownArgs(t *testing.T) {
	t.Parallel()
	if _, err := execCLI(t, "definitely-not-a-command"); err == nil {
		t.Fatal("want error for unknown command")
	}
}

// The two ephemeral-app commands share a named in-memory database, so they
// run sequentially in one test rather than in parallel.
func TestModulesAndOpenAPICommands(t *testing.T) {
	out, err := execCLI(t, "modules", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	if err := json.Unmarshal([]byte(out), &ids); err != nil {
		t.Fatalf("modules --json output %q: %v", out, err)
	}
	if len(ids) != 9 {
		t.Fatalf("modules = %v, want the nine-module starter set", ids)
	}
	found := false
	for _, id := range ids {
		if id == "user_management" {
			found = true
		}
	}
	if !found {
		t.Fatalf("modules = %v, want user_management present", ids)
	}

	spec, err := execCLI(t, "openapi")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(spec, "openapi") {
		t.Fatalf("openapi output does not look like an OpenAPI document: %.120s", spec)
	}
}
