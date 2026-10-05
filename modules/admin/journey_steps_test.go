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
	specs := []string{"account-language.spec.ts", "tenant-languages-design.spec.ts",
		"tenant-languages-type-scale.spec.ts"}
	for _, name := range specs {
		spec, err := os.ReadFile("../../e2e/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(spec), "./steps/admin") {
			t.Errorf("%s does not compose the admin module's exported steps", name)
		}
		if strings.Contains(string(spec), "getByLabel('Password')") {
			t.Errorf("%s types the sign-in fields the exported signIn step already owns", name)
		}
	}
}
