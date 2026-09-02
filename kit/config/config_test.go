package config_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
)

// example is the file the README tells a reader to copy, so it is the file the
// test loads: a drifted example is a broken first five minutes.
const example = "../../config.example.yaml"

func TestLoadExample(t *testing.T) {
	got, err := config.Load(example)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Server.Addr != ":8080" {
		t.Errorf("server.addr = %q, want \":8080\"", got.Server.Addr)
	}
	if got.Server.PublicHost == "" || got.Database.URL == "" ||
		got.Database.MigrateURL == "" || got.NATS.URL == "" || got.Log.Level == "" {
		t.Errorf("a key is missing from %s: %+v", example, got)
	}
}

func TestEnvironmentOverrides(t *testing.T) {
	t.Setenv("PLATFORMKIT_SERVER_ADDR", ":9090")
	got, err := config.Load(example)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Server.Addr != ":9090" {
		t.Errorf("server.addr = %q, want the override \":9090\"", got.Server.Addr)
	}
}

func TestEmptyValueIsRejected(t *testing.T) {
	t.Setenv("PLATFORMKIT_NATS_URL", "")
	if _, err := config.Load(example); err == nil {
		t.Error("Load accepted an empty nats.url")
	}
}

// TestLevelAndURLsAreValidated. Both are mistakes a deployment makes once and
// discovers hours later: a level nothing matches silently logs at info, and a
// URL with the wrong scheme fails four steps away from the key that holds it.
func TestLevelAndURLsAreValidated(t *testing.T) {
	for _, tt := range []struct{ env, value, want string }{
		{"PLATFORMKIT_LOG_LEVEL", "verbose", "log.level"},
		{"PLATFORMKIT_DATABASE_URL", "mysql://localhost/x", "database.url"},
		{"PLATFORMKIT_DATABASE_MIGRATE_URL", "://nope", "database.migrate_url"},
	} {
		t.Run(tt.env, func(t *testing.T) {
			t.Setenv(tt.env, tt.value)
			_, err := config.Load(example)
			if err == nil {
				t.Fatalf("Load accepted %s=%q", tt.env, tt.value)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("the error does not name %s: %v", tt.want, err)
			}
		})
	}

	// postgresql:// is the same scheme spelled out, and is accepted.
	t.Setenv("PLATFORMKIT_DATABASE_URL", "postgresql://u:p@localhost:5432/db?sslmode=disable")
	if _, err := config.Load(example); err != nil {
		t.Errorf("Load rejected postgresql://: %v", err)
	}
}
