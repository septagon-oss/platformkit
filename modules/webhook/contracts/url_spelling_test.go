package contracts_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

// TestNormalizeURLKeepsEveryDestinationItWasHanded pins the one rule the whole
// function has: scheme and host are re-spelled, the default port is dropped, and
// nothing else moves. Each case is a destination a caller could register, and the
// two that a normalisation once changed are here as themselves — a trailing slash,
// which is part of a path and not of the syntax, and an IPv6 address with a port,
// whose brackets belong around the address and not around the pair.
func TestNormalizeURLKeepsEveryDestinationItWasHanded(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ raw, want string }{
		{"HTTPS://Hooks.Example.COM/platformkit", "https://hooks.example.com/platformkit"},
		{"https://hooks.example.com:443/hooks", "https://hooks.example.com/hooks"},
		{"https://hooks.example.com:8443/hooks", "https://hooks.example.com:8443/hooks"},
		{"https://hooks.example.com/", "https://hooks.example.com"},
		{"https://hooks.example.com/hooks/", "https://hooks.example.com/hooks/"},
		{"https://hooks.example.com/hooks/v2/", "https://hooks.example.com/hooks/v2/"},
		{"https://[::1]:32943/hook", "https://[::1]:32943/hook"},
		{"https://[2001:DB8::1]:8443/hook", "https://[2001:db8::1]:8443/hook"},
		{"https://[2001:db8::1]/hook", "https://[2001:db8::1]/hook"},
	} {
		got, err := contracts.NormalizeURL(tt.raw)
		if err != nil {
			t.Errorf("NormalizeURL(%q): %v", tt.raw, err)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}
