//go:build unix

package wire_test

import (
	"os"
	"syscall"
	"testing"
)

func goldenWriterWithoutRoot(t *testing.T) {
	t.Helper()
	// Root bypasses the fixture's read-only mode in CI. Only this disposable
	// child changes identity; the parent still owns and cleans up its files.
	if os.Geteuid() == 0 {
		if err := syscall.Setuid(65534); err != nil {
			t.Fatalf("drop root for the read-only golden fixture: %v", err)
		}
	}
}
