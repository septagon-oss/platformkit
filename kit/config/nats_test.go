package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
)

func TestNATSOwnedSettingsUseFileCompositionAndEnvironmentPrecedence(t *testing.T) {
	body, err := os.ReadFile(example)
	if err != nil {
		t.Fatal(err)
	}
	// Restrict replacements to the NATS block; mail also has credentials.
	start := strings.Index(string(body), "nats:\n")
	end := strings.Index(string(body)[start:], "\nlog:") + start
	path := filepath.Join(t.TempDir(), "config.yaml")
	block := "nats:\n  transport: jetstream\n  url: tls://file.example:4222\n  username: file-user\n  password: file-password\n  ca_cert: file.pem\n"
	if err := os.WriteFile(path, []byte(string(body[:start])+block+string(body[end:])), 0o600); err != nil {
		t.Fatal(err)
	}
	want := config.NATS{Transport: "jetstream", URL: "tls://file.example:4222", Username: "file-user", Password: "file-password", CACert: "file.pem"}
	got, err := config.Load(path)
	if err != nil || got.NATS != want {
		t.Fatalf("file settings were not loaded: %v", err)
	}
	var overrides []config.Override
	for _, field := range []struct{ key, value string }{
		{"transport", "memory"},
		{"url", "tls://environment.example:4222"},
		{"username", "environment-user"},
		{"password", "environment-password"},
		{"ca_cert", "environment.pem"},
	} {
		overrides = append(overrides, config.Set("nats."+field.key, field.value))
	}
	want = config.NATS{Transport: "memory", URL: "tls://environment.example:4222", Username: "environment-user", Password: "environment-password", CACert: "environment.pem"}
	got, err = config.Load(path, overrides...)
	if err != nil || got.NATS != want {
		t.Fatalf("composition did not override file settings: %v", err)
	}
	for _, field := range []struct{ env, value string }{
		{"TRANSPORT", "jetstream"}, {"URL", "tls://final.example:4222"},
		{"USERNAME", "final-user"}, {"PASSWORD", "final-password"}, {"CA_CERT", "final.pem"},
	} {
		t.Setenv("PLATFORMKIT_NATS_"+field.env, field.value)
	}
	want = config.NATS{Transport: "jetstream", URL: "tls://final.example:4222", Username: "final-user", Password: "final-password", CACert: "final.pem"}
	got, err = config.Load(path, overrides...)
	if err != nil || got.NATS != want {
		t.Fatalf("environment did not override composition settings: %v", err)
	}
}

func TestNATSRefusesUnsafeOrAmbiguousSettingsWithoutEchoingSecrets(t *testing.T) {
	for _, test := range []struct {
		name string
		cfg  config.NATS
	}{
		{"mode", config.NATS{URL: "nats://localhost:4222", Transport: "unknown"}},
		{"no endpoint", config.NATS{}},
		{"malformed", config.NATS{URL: "tls://credential-canary@[broken"}},
		{"embedded auth", config.NATS{URL: "tls://user:credential-canary@broker.example:4222", Username: "other", Password: "credential-canary"}},
		{"path", config.NATS{URL: "tls://broker.example/credential-canary"}},
		{"query", config.NATS{URL: "tls://broker.example?token=credential-canary"}},
		{"scheme", config.NATS{URL: "https://broker.example:4222"}},
		{"username only", config.NATS{URL: "tls://broker.example:4222", Username: "credential-canary"}},
		{"password only", config.NATS{URL: "tls://broker.example:4222", Password: "credential-canary"}},
		{"remote plaintext", config.NATS{URL: "nats://broker.example:4222", Username: "user", Password: "credential-canary"}},
		{"CA without TLS", config.NATS{URL: "nats://localhost:4222", CACert: "credential-canary.pem"}},
		{"mixed security", config.NATS{URL: "tls://broker.example:4222,nats://other.example:4222", Username: "user", Password: "credential-canary"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := config.Load(example,
				config.Set("nats.url", test.cfg.URL), config.Set("nats.transport", test.cfg.Transport),
				config.Set("nats.username", test.cfg.Username), config.Set("nats.password", test.cfg.Password),
				config.Set("nats.ca_cert", test.cfg.CACert))
			if err == nil || !strings.Contains(err.Error(), "nats.") || strings.Contains(err.Error(), "credential-canary") {
				t.Fatal("invalid settings must fail with a credential-free diagnostic naming the NATS key")
			}
		})
	}
}

func TestNATSAcceptsPrivateTLSAndLocalDevelopmentConnections(t *testing.T) {
	for _, endpoint := range []string{
		"nats://localhost:4222", "nats://127.0.0.1:4222", "nats://[::1]:4222",
		"tls://broker.example:4222", "tls://first.example:4222, tls://second.example:4222",
	} {
		settings := config.NATS{URL: endpoint, Username: "user", Password: "password"}
		if strings.HasPrefix(endpoint, "tls://") {
			settings.CACert = "a-file-read-only-by-the-provider.pem"
		}
		if err := settings.Validate(); err != nil {
			t.Errorf("valid connection rejected: %v", err)
		}
	}
	if err := (config.NATS{URL: "nats://development-broker:4222"}).Validate(); err != nil {
		t.Errorf("existing unauthenticated development connection rejected: %v", err)
	}
}
