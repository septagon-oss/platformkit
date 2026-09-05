package config_test

import (
	"os"
	"path/filepath"
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
	if _, err := config.Load(filepath.Join(t.TempDir(), "config.yaml")); err == nil || !strings.Contains(err.Error(), "config.example.yaml") {
		t.Errorf("a missing config.yaml should be told where to get one, got %v", err)
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

// TestAnOverrideIsAppliedBeforeValidation is the defect this signature closes.
//
// A client overlay used to set server.public_host and mail.from after Load had
// returned: Load required both to be non-empty before the values that replaced
// them were read, and then validated a string that was about to be thrown away.
// So the file had to carry a host it would never be reached at, and the host it
// was reached at was the one nothing checked.
func TestAnOverrideIsAppliedBeforeValidation(t *testing.T) {
	cfg := config.Set("server.public_host", "collect.example.com")
	got, err := config.Load(example, cfg)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Server.PublicHost != "collect.example.com" {
		t.Errorf("server.public_host = %q, want the override", got.Server.PublicHost)
	}

	// The file need not carry a value it will never be reached at. This is the
	// half that could not work before: server.public_host is required, so a
	// file that left it to the overlay was refused as empty before the overlay
	// was consulted.
	if _, err := config.Load(hostless(t), cfg); err != nil {
		t.Errorf("Load refused a file whose host the overlay supplies: %v", err)
	}
	if _, err := config.Load(hostless(t)); err == nil {
		t.Error("Load accepted a file with no server.public_host and no override")
	}

	// mail.from has no environment override, and is still overridable: it is a
	// value a client's overlay knows and the shared file cannot.
	got, err = config.Load(mailServer(t), config.Set("mail.from", "noreply@collect.example.com"))
	if err != nil {
		t.Fatalf("Load with a sender: %v", err)
	}
	if got.Mail.From != "noreply@collect.example.com" {
		t.Errorf("mail.from = %q, want the override", got.Mail.From)
	}
}

// TestAnInvalidOverrideIsRefusedByName, with the two values the review found in
// a client overlay. Both used to be accepted: public_host was checked for
// emptiness and nothing else, and mail.from for emptiness only when a host was
// set beside it.
func TestAnInvalidOverrideIsRefusedByName(t *testing.T) {
	for _, tt := range []struct{ key, value, want string }{
		{"server.public_host", "http://not a host/with/a/path", "server.public_host"},
		{"server.public_host", "https://acme.example.com", "server.public_host"},
		{"server.public_host", "acme.example.com/admin", "server.public_host"},
		{"mail.from", "this is not an address", "mail.from"},
		{"mail.from", "Acme <noreply@acme.example.com>", "mail.from"},
	} {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			_, err := config.Load(mailServer(t), config.Set(tt.key, tt.value))
			if err == nil {
				t.Fatalf("Load accepted %s=%q", tt.key, tt.value)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("the error does not name %s: %v", tt.want, err)
			}
		})
	}

	// A key nothing holds is refused by name too, rather than applied to
	// nothing: an overlay that misspells a key would otherwise run with the
	// file's value and no sign that it asked for another.
	_, err := config.Load(example, config.Set("server.public_hostname", "acme.example.com"))
	if err == nil || !strings.Contains(err.Error(), "server.public_hostname") {
		t.Errorf("an unknown key was not refused by name: %v", err)
	}
}

// TestTheEnvironmentOutranksAComposition. An override belongs to whoever wrote
// the overlay; the environment belongs to whoever is running the process, and
// PLATFORMKIT_SERVER_PUBLIC_HOST was dead in the flagship binary precisely
// because code applied its own value afterwards.
func TestTheEnvironmentOutranksAComposition(t *testing.T) {
	t.Setenv("PLATFORMKIT_SERVER_PUBLIC_HOST", "staging.example.com")
	got, err := config.Load(example, config.Set("server.public_host", "collect.example.com"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Server.PublicHost != "staging.example.com" {
		t.Errorf("server.public_host = %q, want the environment's value", got.Server.PublicHost)
	}
}

// hostless is config.example.yaml with no server.public_host: the shape a
// client overlay's own file has, because the host is the overlay's to know.
func hostless(t *testing.T) string {
	t.Helper()
	return edit(t, `public_host: "platformkit.localhost:8080"`, `public_host: ""`)
}

// mailServer is config.example.yaml with a mail server configured, so that the
// sender is validated rather than refused for having no host beside it.
func mailServer(t *testing.T) string {
	t.Helper()
	return edit(t, "mail:\n  host: \"\"", "mail:\n  host: \"smtp.example.com\"")
}

// edit is config.example.yaml with one line replaced, so the case under test is
// the only thing that differs from the file the README tells a reader to copy.
func edit(t *testing.T, from, to string) string {
	t.Helper()
	body, err := os.ReadFile(example)
	if err != nil {
		t.Fatalf("read %s: %v", example, err)
	}
	out := strings.Replace(string(body), from, to, 1)
	if out == string(body) {
		t.Fatalf("config.example.yaml no longer contains %q; this test edits it by hand", from)
	}
	path := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatalf("write the config: %v", err)
	}
	return path
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
