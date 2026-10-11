package internal

import (
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
)

// An assertion's signature is checked by goxmldsig (crewjam/saml's ParseXMLResponse
// reaches its validation context). Releases before v1.6.1 carry GO-2026-4753, a
// signature bypass, so the build this package is tested in must not link one.
func TestTheAssertionSignatureVerifierIsAReleaseWithoutTheKnownBypass(t *testing.T) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("the test binary carries no build information")
	}
	const path = "github.com/russellhaering/goxmldsig"
	for _, dep := range info.Deps {
		if dep.Path != path {
			continue
		}
		if dep.Replace != nil {
			dep = dep.Replace
		}
		if !atLeast(t, dep.Version, [3]int{1, 6, 1}) {
			t.Fatalf("%s %s links a signature verifier with a known bypass; want v1.6.1 or later", path, dep.Version)
		}
		return
	}
	t.Fatalf("%s is not linked, so no assertion signature is verified by the release this pins", path)
}

func atLeast(t *testing.T, version string, floor [3]int) bool {
	t.Helper()
	core, _, _ := strings.Cut(strings.TrimPrefix(version, "v"), "-")
	parts := strings.SplitN(core, ".", 3)
	if len(parts) != 3 {
		t.Fatalf("unreadable module version %q", version)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("unreadable module version %q", version)
		}
		if n != floor[i] {
			return n > floor[i]
		}
	}
	return true
}
