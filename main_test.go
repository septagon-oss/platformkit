// Validates: REQ-016.
// Per: ADR-0017.
// Discipline: C-14.
package main

import (
	"testing"

	"github.com/septagon-oss/pk-apps/pkg/starterapp"
)

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
			applyAddressOverrides(cfg, func(key string) string { return tc.env[key] })
			if cfg.HTTP.Addr != tc.want {
				t.Fatalf("address = %q, want %q", cfg.HTTP.Addr, tc.want)
			}
		})
	}
}
