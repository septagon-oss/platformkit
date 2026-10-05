package ui_test

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// Run the shipped controllers with their vendored htmx, not a simulated DOM.
// Browser dependencies are the ones the repository's e2e suite already uses.
func runCommandBrowser(t *testing.T, scenario string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("command browser case needs Node and the e2e Playwright installation")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "testdata/command_browser.cjs", scenario)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command browser %s: %v\n%s", scenario, err, out)
	}
	t.Logf("%s", out)
}
