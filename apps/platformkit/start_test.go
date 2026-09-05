package main

import (
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
