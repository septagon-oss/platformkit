package components_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSharedWebCasesUseBehaviorNames(t *testing.T) {
	root := filepath.Join("..", "..")
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=A", "origin/main...HEAD", "--", "ui/components", "ui/resource", "e2e", "tools/designexport/openpencil/browser")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list added cases: %v: %s", err, output)
	}
	titleLabel := regexp.MustCompile(`(?m)(?:func\s+Test(?:Review|Round|Probe)[A-Z0-9_]|(?:test|describe)\s*\(\s*['"](?:review|round|probe)(?:\b|[_ -]))`)
	for _, path := range strings.Fields(string(output)) {
		if !strings.HasSuffix(path, "_test.go") && !strings.HasSuffix(path, ".spec.ts") && !strings.HasSuffix(path, ".test.mjs") {
			continue
		}
		name := strings.ToLower(filepath.Base(path))
		for _, label := range []string{"review", "round", "probe"} {
			if strings.HasPrefix(name, label+"_") || strings.HasPrefix(name, label+"-") {
				t.Errorf("case filename names its occasion instead of behavior: %s", path)
				break
			}
		}
		body, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if titleLabel.Match(body) {
			t.Errorf("case title names its occasion instead of behavior: %s", path)
		}
	}
}
