// Validates: REQ-016.
// Per: ADR-0017.
// Discipline: C-14.
package main

import (
	"context"
	"errors"
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
