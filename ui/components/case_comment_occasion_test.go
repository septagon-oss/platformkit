package components_test

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSharedWebCasesDoNotSayWhichReviewWroteThem: a case added by this branch
// says what it holds, never which review or round asked for it — in its name or
// in its comments (decision 0072). TestSharedWebCasesUseBehaviorNames reads the
// names; this reads the comment lines of the same added cases, and of the
// composition's case the branch added beside them.
func TestSharedWebCasesDoNotSayWhichReviewWroteThem(t *testing.T) {
	root := filepath.Join("..", "..")
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=A", "origin/main...HEAD", "--",
		"ui/components", "ui/resource", "e2e", "apps/platformkit", "kit/db")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list added cases: %v: %s", err, output)
	}
	occasion := regexp.MustCompile(`(?i)\bround[- ]?[0-9]+\b|\b(deferred|adversarial)\s+review\b|\b[a-z]+(st|nd|rd|th)\s+review\b|\breview\s+(round\s+)?[0-9]+\b`)
	for _, path := range strings.Fields(string(output)) {
		if !strings.HasSuffix(path, "_test.go") && !strings.HasSuffix(path, ".spec.ts") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		scanner := bufio.NewScanner(bytes.NewReader(body))
		for line := 1; scanner.Scan(); line++ {
			text := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(text, "//") && occasion.MatchString(text) {
				t.Errorf("%s:%d: a comment says which review wrote the case: %s", path, line, text)
			}
		}
	}
}
