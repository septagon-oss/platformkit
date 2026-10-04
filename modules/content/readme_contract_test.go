package content_test

import (
	"os"
	"strings"
	"testing"
)

func TestContentREADMEStatesInterfaceVerificationAndLimits(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, heading := range []string{"## Interface", "## Verification", "## Limits"} {
		if !strings.Contains(string(readme), heading+"\n") {
			t.Errorf("content README has no %s section", heading)
		}
	}
}
