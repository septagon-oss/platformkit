package ui_test

import "testing"

func TestAKeyReuseRefusalRetainsTheCommandFormAndItsInput(t *testing.T) {
	runCommandBrowser(t, "refusal")
}
