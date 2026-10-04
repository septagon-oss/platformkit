package admin

import (
	"os"
	"strings"
	"testing"
)

func TestAdminLanguageJourneyUsesExportedSteps(t *testing.T) {
	steps, err := os.ReadFile("../../e2e/steps/admin.ts")
	if err != nil {
		t.Fatalf("the admin module's browser steps are not exported: %v", err)
	}
	if !strings.Contains(string(steps), "export ") {
		t.Fatal("the admin steps file exports no reusable journey step")
	}
	spec, err := os.ReadFile("../../e2e/account-language.spec.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(spec), "./steps/admin") {
		t.Error("the account-language journey does not compose the admin module's exported steps")
	}
}
