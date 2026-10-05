package ui_test

import "testing"

func TestAnAutomaticRetryKeepsTheSubmittedBodyDespiteLaterEdits(t *testing.T) {
	runInterruptedCommandBrowser(t, "edited-backoff")
}
