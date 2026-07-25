// Validates: REQ-016.
// Per: ADR-0017.
// Discipline: C-14.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/septagon-oss/pk-apps/pkg/starterapp"
)

func TestRunAppliesAddressOverridesAndDelegates(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("stop after configuration")
	err := run(
		context.Background(),
		func(key string) string {
			if key == "PORT" {
				return "9090"
			}
			return ""
		},
		func(_ context.Context, cfg *starterapp.Config, _ ...starterapp.Option) error {
			if cfg.HTTP.Addr != "127.0.0.1:9090" {
				t.Fatalf("address = %q, want loopback override", cfg.HTTP.Addr)
			}
			return wantErr
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("run error = %v, want %v", err, wantErr)
	}
}

func TestAddressOverridesStayLoopbackUnlessExplicit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "default", want: "127.0.0.1:8080"},
		{name: "port remains local", env: map[string]string{"PORT": "9090"}, want: "127.0.0.1:9090"},
		{
			name: "explicit address wins",
			env:  map[string]string{"PORT": "9090", "PK_HTTP_ADDR": "0.0.0.0:8081"},
			want: "0.0.0.0:8081",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := starterapp.DefaultConfig()
			starterapp.ApplyAddressOverrides(cfg, func(key string) string { return tc.env[key] })
			if cfg.HTTP.Addr != tc.want {
				t.Fatalf("address = %q, want %q", cfg.HTTP.Addr, tc.want)
			}
		})
	}
}

func TestRunLoadsConfigNamedByPKConfig(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("app_name: acme\nseed:\n  admin_password: long-enough-secret-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("stop after configuration")
	err := run(
		context.Background(),
		func(key string) string {
			if key == "PK_CONFIG" {
				return path
			}
			return ""
		},
		func(_ context.Context, cfg *starterapp.Config, _ ...starterapp.Option) error {
			if cfg.AppName != "acme" {
				t.Fatalf("AppName = %q, want value from config file", cfg.AppName)
			}
			if cfg.Environment != "production" {
				t.Fatalf("Environment = %q, want fail-closed production for a configured deployment", cfg.Environment)
			}
			return wantErr
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("run error = %v, want %v", err, wantErr)
	}
}

func TestRunFindsConfigYamlInWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("environment: development\napp_name: from-cwd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	wantErr := errors.New("stop after configuration")
	err := run(
		context.Background(),
		func(string) string { return "" },
		func(_ context.Context, cfg *starterapp.Config, _ ...starterapp.Option) error {
			if cfg.AppName != "from-cwd" {
				t.Fatalf("AppName = %q, want config.yaml discovery from the working directory", cfg.AppName)
			}
			return wantErr
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("run error = %v, want %v", err, wantErr)
	}
}

func TestRunZeroConfigStaysDevelopment(t *testing.T) {
	t.Chdir(t.TempDir())
	wantErr := errors.New("stop after configuration")
	err := run(
		context.Background(),
		func(string) string { return "" },
		func(_ context.Context, cfg *starterapp.Config, _ ...starterapp.Option) error {
			if cfg.Environment != "development" {
				t.Fatalf("Environment = %q, want zero-config development mode", cfg.Environment)
			}
			return wantErr
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("run error = %v, want %v", err, wantErr)
	}
}
