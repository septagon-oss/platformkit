package ui_test

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// A submission whose answer was lost, resumed by one press after a reload, is
// answered from the server's record and is not sent again under a new key: the
// person pressed once for a command that had already run.
func TestAReplayedAnswerAfterAReloadIsNotASecondCommand(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("command browser case needs Node and the e2e Playwright installation")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "node", "testdata/command_replay_browser.cjs").CombinedOutput()
	if err != nil {
		t.Fatalf("command replay after reload: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
