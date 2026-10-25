package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestKitSourcesCarryNoLintSuppression pins that the readable-steps split of
// kit/ shortened functions by structure and not by silencing a linter.
func TestKitSourcesCarryNoLintSuppression(t *testing.T) {
	root := filepath.Join("..")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), "//nolint") {
			t.Errorf("%s carries a //nolint directive", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
