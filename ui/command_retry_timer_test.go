package ui_test

import "testing"

func TestASuccessfulManualRetryCancelsThePendingAutomaticRetry(t *testing.T) {
	runCommandBrowser(t, "timer")
}
