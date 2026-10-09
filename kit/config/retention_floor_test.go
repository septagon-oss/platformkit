package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
)

// exampleWith is config.example.yaml with one line replaced, so the case under test
// is the only thing that differs from the file the README tells a reader to copy.
func exampleWith(t *testing.T, old, replacement string) string {
	t.Helper()
	body, err := os.ReadFile(example)
	if err != nil {
		t.Fatalf("read %s: %v", example, err)
	}
	out := strings.Replace(string(body), old, replacement, 1)
	if out == string(body) {
		t.Fatalf("config.example.yaml no longer holds %q; this test edits it by hand", old)
	}
	path := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatalf("write the config: %v", err)
	}
	return path
}

// TestARetentionPeriodBelowTheFloorIsRefusedAtBoot holds config.Load to the floor
// migrations/000048 installs: a shorter period is refused and names the floor, the
// floor itself boots, and the expiry role's DSN is read from the file.
func TestARetentionPeriodBelowTheFloorIsRefusedAtBoot(t *testing.T) {
	_, err := config.Load(exampleWith(t, "retention_days: 365", "retention_days: 30"))
	if err == nil || !strings.Contains(err.Error(), "365") {
		t.Errorf("a 30-day retention period loaded: %v", err)
	}
	cfg, err := config.Load(exampleWith(t, `retain_url: ""`,
		`retain_url: "postgres://platformkit_retain:pw@localhost:5432/platformkit?sslmode=disable"`))
	if err != nil {
		t.Fatalf("the example with an expiry role: %v", err)
	}
	if cfg.Audit.RetentionDays != 365 || !strings.HasPrefix(cfg.Database.RetainURL, "postgres://platformkit_retain:") {
		t.Errorf("loaded retention %d days and retain_url %q", cfg.Audit.RetentionDays, cfg.Database.RetainURL)
	}
}

// TestAMalformedRetainURLDoesNotEchoItsPassword: the expiry role's DSN carries a
// password, and the refusal of a malformed one names the key, not the secret.
func TestAMalformedRetainURLDoesNotEchoItsPassword(t *testing.T) {
	_, err := config.Load(exampleWith(t, `retain_url: ""`,
		`retain_url: "postgres://platformkit_retain:hunter2%zz@localhost:5432/platformkit"`))
	if err == nil {
		t.Fatal("a malformed retain_url loaded")
	}
	if !strings.Contains(err.Error(), "database.retain_url") {
		t.Errorf("the refusal does not name the key: %v", err)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("the refusal echoes the password: %v", err)
	}
}
