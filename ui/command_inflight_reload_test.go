package ui_test

import "testing"

func TestAReloadBeforeTheNetworkFailureKeepsThePendingCommandKey(t *testing.T) {
	runInterruptedCommandBrowser(t, "inflight-reload")
}
