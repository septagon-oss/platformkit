package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The start command's configuration is the development one with the database's
// port in it; the two connection strings and the public host are the values a
// mistake would land in.
func TestStartConfigCarriesThePortsAndTheDataDirectory(t *testing.T) {
	t.Parallel()
	cfg := startConfig("var/pk", ":9090", 54321)
	if cfg.Server.PublicHost != "platformkit.localhost:9090" || cfg.Server.Addr != ":9090" {
		t.Fatalf("server = %+v", cfg.Server)
	}
	for _, url := range []string{cfg.Database.URL, cfg.Database.MigrateURL} {
		if !strings.Contains(url, "127.0.0.1:54321/platformkit") {
			t.Fatalf("database url %q does not name the embedded port", url)
		}
	}
	if !strings.HasPrefix(cfg.Database.URL, "postgres://platformkit_app:") || !strings.HasPrefix(cfg.Database.MigrateURL, "postgres://postgres:") {
		t.Fatalf("the two roles are wrong: %s / %s", cfg.Database.URL, cfg.Database.MigrateURL)
	}
	if cfg.Files.Dir != "var/pk/files" || cfg.Log.Level != "info" || cfg.Audit.RetentionDays != 365 {
		t.Fatalf("defaults = files %q log %q audit %d", cfg.Files.Dir, cfg.Log.Level, cfg.Audit.RetentionDays)
	}
}

// startWebhookKey is the other half of the pair `make run` boots with, and the
// two spellings are one key. config.example.yaml is what `make run` reads and the
// constant is what `start` boots with; an installation that registered endpoints on
// one and restarted on the other could not open the secrets it already wrapped, so
// the value the two files carry is read here and compared rather than trusted to
// whoever edited them last.
func TestStartSigningKeyIsTheKeyTheExampleCarries(t *testing.T) {
	t.Parallel()
	file, err := os.ReadFile(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	example := ""
	for _, line := range strings.Split(string(file), "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "secret_key:") {
			if example != "" {
				t.Fatalf("config.example.yaml names secret_key twice; this case reads one")
			}
			example = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "secret_key:")), "\"")
		}
	}
	if example == "" {
		t.Fatal("config.example.yaml names no secret_key; this case reads it by hand")
	}
	if got := startConfig("var/pk", ":9090", 54321).Webhook.SecretKey; got != example {
		t.Errorf("`start` wraps secrets with %q and `make run` with %q", got, example)
	}
	if len(startWebhookKey) != 32 {
		t.Errorf("the key is %d bytes and the module asks for 32", len(startWebhookKey))
	}
}

// The embedded init file is executed one statement at a time; the split has to
// drop the comments and keep every statement, or the role is half made.
func TestPostgresInitSplitsIntoItsStatements(t *testing.T) {
	t.Parallel()
	got := statements(postgresInit)
	if len(got) != 4 {
		t.Fatalf("%d statements, want 4:\n%s", len(got), strings.Join(got, "\n---\n"))
	}
	if !strings.HasPrefix(got[0], "CREATE ROLE platformkit_app") || !strings.Contains(got[0], "NOBYPASSRLS") {
		t.Fatalf("the first statement is not the role: %q", got[0])
	}
	for _, s := range got {
		if strings.Contains(s, "--") {
			t.Fatalf("a comment survived the split: %q", s)
		}
	}
}
