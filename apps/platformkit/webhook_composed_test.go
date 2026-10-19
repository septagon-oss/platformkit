package main

import (
	"github.com/septagon-oss/platformkit/pkit"
	"testing"
)

// TestTheReferenceCompositionPlacesTheWebhookModule: webhook is a kernel module,
// and a tenant administrator registers endpoints on screens generated from its
// Spec. Like audit, task and notification it is named in Use; a module no
// composition places has no route, no screen and no subscriber.
func TestTheReferenceCompositionPlacesTheWebhookModule(t *testing.T) {
	cfg := referenceCompositionConfig(t)
	r := composeReference(cfg, pkit.Development)
	if !namesAModule(r.modules, "webhook") {
		t.Fatalf("the reference composition places no webhook module: %v", moduleNames(r.modules))
	}
}
