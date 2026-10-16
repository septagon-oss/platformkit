package main

// The reference application is decision 0074's pilot: it is composed in pkit's
// sentences, in app.go, and the process commits what that composition resolves
// to as COMPOSITION.<env>.md for development and production, so a reader can
// diff what the application is without running it.

import (
	"os"
	"strings"
	"testing"
)

func TestTheReferenceAppIsComposedInSentences(t *testing.T) {
	src, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatalf("the reference application has no app.go: %v", err)
	}
	for _, word := range []string{"pkit.NewApp(", ".Use("} {
		if !strings.Contains(string(src), word) {
			t.Errorf("app.go does not compose the application with %s", word)
		}
	}
}

func TestTheCompositionFileIsCommittedForEachEnvironment(t *testing.T) {
	for _, env := range []string{"development", "production"} {
		text, err := os.ReadFile("COMPOSITION." + env + ".md")
		if err != nil {
			t.Errorf("COMPOSITION.%s.md is not committed: %v", env, err)
			continue
		}
		if !strings.Contains(string(text), "# COMPOSITION — ") {
			t.Errorf("COMPOSITION.%s.md is not the text Server.Explain writes", env)
		}
	}
}
