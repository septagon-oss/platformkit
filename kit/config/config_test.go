package config_test

import (
	"os"
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

// TestTheRedirectPathHasToBeUnderTheAuthPrefix.
//
// The callback route is mounted by taking the auth module's own prefix off this
// path and putting it back, so a path outside the prefix is a route on a prefix
// the module does not own — and one shorter than the prefix used to be a slice
// out of range, which is a panic at boot for a typo in a YAML file. Both of the
// values below came out of E3.1's review.
func TestTheRedirectPathHasToBeUnderTheAuthPrefix(t *testing.T) {
	for _, bad := range []string{"/callback", "/oauth2/callback/here", "/api/v1/authx/cb", "/api/v1/auth"} {
		t.Run(bad, func(t *testing.T) {
			cfg := write(t, `redirect_path: "`+bad+`"`)
			_, err := config.Load(cfg)
			if err == nil {
				t.Fatalf("Load accepted auth.oidc.redirect_path %q", bad)
			}
			if !strings.Contains(err.Error(), "auth.oidc.redirect_path") {
				t.Errorf("the error does not name the key: %v", err)
			}
		})
	}
	// The default, and one under the prefix that is not the default, are both
	// accepted: a provider registered against another path is the whole reason
	// the key exists.
	for _, good := range []string{`redirect_path: ""`, `redirect_path: "/api/v1/auth/sso/back"`} {
		if _, err := config.Load(write(t, good)); err != nil {
			t.Errorf("Load rejected %s: %v", good, err)
		}
	}
}

// write is config.example.yaml with an identity provider configured and one of
// its keys replaced, so the case under test is the only thing that differs from
// the file the README tells a reader to copy.
func write(t *testing.T, redirect string) string {
	t.Helper()
	body, err := os.ReadFile(example)
	if err != nil {
		t.Fatalf("read %s: %v", example, err)
	}
	provider := "auth:\n  oidc:\n    issuer: \"https://idp.example\"\n" +
		"    client_id: \"platformkit\"\n    client_secret: \"secret\"\n    " + redirect + "\n"
	out := strings.Replace(string(body), `auth:
  oidc:
    issuer: ""
    client_id: ""
    client_secret: ""
    redirect_path: "/api/v1/auth/oidc/callback"
`, provider, 1)
	if out == string(body) {
		t.Fatal("the auth block of config.example.yaml has moved; this test edits it by hand")
	}
	path := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatalf("write the config: %v", err)
	}
	return path
}
