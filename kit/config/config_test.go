package config_test

import (
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
