package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/pkit"
)

// TestTheReferenceCompositionPlacesTheWebhookModule: webhook is a kernel module,
// and a tenant administrator registers endpoints on screens generated from its
// Spec. Like audit, task and notification it is named in Use; a module no
// composition places has no route, no screen and no subscriber.
func TestTheReferenceCompositionPlacesTheWebhookModule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(compositionConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	r := composeReference(cfg, pkit.Development)
	if !namesAModule(r.modules, "webhook") {
		t.Fatalf("the reference composition places no webhook module: %v", moduleNames(r.modules))
	}
}
