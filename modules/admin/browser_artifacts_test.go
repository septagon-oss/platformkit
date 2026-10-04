package admin_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestBrowserRunStatusIsNotPublished(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "git", "-C", "../..", "ls-files", "--", "test-results/.last-run.json").CombinedOutput()
	if err != nil {
		t.Fatalf("read tracked browser artifact: %v: %s", err, output)
	}
	if strings.TrimSpace(string(output)) != "" {
		t.Errorf("generated browser run status is tracked: %s", output)
	}
}
