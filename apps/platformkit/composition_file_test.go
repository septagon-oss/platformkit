package main

// The composition file is decision 0074 rule 4's deliverable: what the reference
// application resolves to, in text, committed, so a reader can diff what the
// application is without running it. It is written by pkit.Server.Explain — the
// resolved composition, the module that provides each port the kernel asks the
// application for, the tenant hosts the process claims — and never by hand.
//
// `make generate` writes the file (`platformkit composition`); a file that
// disagrees with the composition is a failure here. That is the whole reason it
// is committed: a review can read the file, and a change to the composition that
// nobody described in words fails a check rather than passing quietly.

import (
	"os"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/pkit"
)

// compositionConfig is the installation the composition files describe: the
// reference deployment's own hosts, committed at a fixed path because a committed
// document cannot be worded around a path that changes on every run. `make
// generate` passes the same file to `platformkit composition`.
const compositionConfig = "testdata/composition.yaml"

func TestCompositionFile(t *testing.T) {
	cfg, err := config.Load(compositionConfig)
	if err != nil {
		t.Fatalf("load the configuration: %v", err)
	}
	for _, env := range []pkit.Environment{pkit.Development, pkit.Production} {
		name := "COMPOSITION." + string(env) + ".md"
		want, _, err := compositionDocuments(cfg, env)
		if err != nil {
			t.Fatalf("%s: the reference composition does not describe itself: %v", name, err)
		}
		got, err := os.ReadFile(name)
		if err != nil {
			t.Errorf("%s is not committed: %v", name, err)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("%s is not what the composition resolves to today; regenerate with make generate\n--- committed ---\n%s\n--- resolved now ---\n%s",
				name, got, want)
		}
	}
}
