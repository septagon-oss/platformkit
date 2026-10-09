package content_test

import (
	"os"
	"strings"
	"testing"
)

func TestContentJourneyExportsReusableBrowserSteps(t *testing.T) {
	steps, err := os.ReadFile("../../e2e/steps/content.ts")
	if err != nil {
		t.Fatalf("content journey steps are unavailable: %v", err)
	}
	if !strings.Contains(string(steps), "export ") {
		t.Fatal("content journey steps expose no function to client specs")
	}
}
