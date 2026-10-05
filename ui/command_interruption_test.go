package ui_test

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func runInterruptedCommandBrowser(t *testing.T, scenario string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("command browser case needs Node and the e2e Playwright installation")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "node", "testdata/command_interruption_browser.cjs", scenario).CombinedOutput()
	if err != nil {
		t.Fatalf("interrupted command %s: %v\n%s", scenario, err, out)
	}
	t.Logf("%s", out)
}
