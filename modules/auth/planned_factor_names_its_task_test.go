package auth_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestAPlannedFactorNamesTheTaskThatFinishesIt holds ARCHITECTURE.md's
// authentication subsection to the rule the section states for itself: a
// pillar only partly on main says what is on main and names the task that
// finishes it. While go.mod names no WebAuthn library, the subsection that says
// WebAuthn is planned must name that task (a `T-NNNN` id).
func TestAPlannedFactorNamesTheTaskThatFinishesIt(t *testing.T) {
	mod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(mod)), "webauthn") {
		t.Skip("go.mod names a WebAuthn library; the factor is no longer planned")
	}
	doc, err := os.ReadFile("../../ARCHITECTURE.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	_, section, ok := strings.Cut(text, "### Authentication")
	if !ok {
		t.Fatal("ARCHITECTURE.md has no `### Authentication` subsection")
	}
	if end := strings.Index(section, "\n### "); end >= 0 {
		section = section[:end]
	}
	if !strings.Contains(section, "WebAuthn") {
		t.Fatal("the authentication subsection does not mention WebAuthn")
	}
	if !regexp.MustCompile(`\bT-\d{4}\b`).MatchString(section) {
		t.Error("the authentication subsection calls WebAuthn planned but names no task that finishes it")
	}
}
