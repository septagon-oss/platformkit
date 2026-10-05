package ui_test

import "testing"

func TestALostSubmissionKeepsItsKeyAndBodyAcrossReload(t *testing.T) {
	runCommandBrowser(t, "reload")
}
