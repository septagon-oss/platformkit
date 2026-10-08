package wire

import (
	"bytes"
	"os"
	"testing"
)

// Golden renders once and checks an existing baseline. A nonempty UPDATE_GOLDEN
// rewrites only a compatible document; it never bootstraps or launders a break.
func Golden(t *testing.T, path string, serve func() []byte) {
	t.Helper()
	GoldenWithAllowances(t, path, serve, nil)
}

// GoldenWithAllowances is Golden with a caller-owned reviewed authorization list.
// Callers must serialize writers to a path. OS write errors are not atomic.
func GoldenWithAllowances(t *testing.T, path string, serve func() []byte, allowances []AuthorizationAllowance) {
	t.Helper()
	current := serve()
	golden := readGolden(t, path)
	refuseWireBreak(t, path, golden, current, allowances)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, current, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("rewrote %s", path)
		return
	}
	if !bytes.Equal(golden, current) {
		t.Fatalf("%s is stale; run with UPDATE_GOLDEN=1.\nfirst difference at byte %d", path, firstDifference(golden, current))
	}
}

func refuseWireBreak(t *testing.T, path string, golden, current []byte, allowances []AuthorizationAllowance) {
	t.Helper()
	problems := CompareWithAllowances(golden, current, allowances)
	for _, problem := range problems {
		t.Error(problem)
	}
	if len(problems) > 0 {
		t.Fatalf("%s is left as it was: the rules above stand, and UPDATE_GOLDEN=1 regenerates a document that is stale, never one that is broken", path)
	}
}

func readGolden(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run with UPDATE_GOLDEN=1)", path, err)
	}
	return body
}

func firstDifference(a, b []byte) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
