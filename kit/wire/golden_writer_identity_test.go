//go:build unix

package wire_test

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestReadOnlyGoldenRefusalPreservesTheParentIdentity(t *testing.T) {
	uid, euid := os.Getuid(), os.Geteuid()
	for _, kind := range []string{"openapi", "asyncapi"} {
		t.Run(kind, func(t *testing.T) {
			old := baseline(kind)
			current := mutated(t, old, func(doc map[string]any) { doc["description"] = "added" })
			output, onDisk, err := runGolden(t, old, current, "1", "readonly")
			if err == nil || !strings.Contains(output, "write golden.json:") || !bytes.Equal(onDisk, old) {
				t.Fatalf("read-only update: err=%v, unchanged=%v\n%s", err, bytes.Equal(onDisk, old), output)
			}
			if os.Getuid() != uid || os.Geteuid() != euid {
				t.Fatal("the golden child changed its parent's identity")
			}
			output, onDisk, err = runGolden(t, old, current, "1", "")
			if err != nil || !bytes.Equal(onDisk, current) {
				t.Fatalf("a subsequent writable update failed: %v\n%s", err, output)
			}
		})
	}
}
