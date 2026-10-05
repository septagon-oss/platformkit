package admin_test

import (
	"os"
	"strings"
	"testing"
)

func TestLanguageDesignJourneysReuseTheAdminSignInStep(t *testing.T) {
	for _, name := range []string{"tenant-languages-design.spec.ts", "tenant-languages-type-scale.spec.ts"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("../../e2e/" + name)
			if err != nil {
				t.Fatal(err)
			}
			spec := string(raw)
			if !strings.Contains(spec, "./steps/admin") {
				t.Error("language design journey does not import the existing admin steps")
			}
			if strings.Contains(spec, "getByLabel('Password').fill(password)") {
				t.Error("language design journey reimplements the sign-in fields already owned by e2e/steps/admin.ts")
			}
		})
	}
}
