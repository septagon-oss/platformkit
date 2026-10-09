package main

import (
	"testing"

	"github.com/septagon-oss/platformkit/pkit"
)

func TestStartConfigCanComposeTheReferenceApplication(t *testing.T) {
	cfg := startConfig(t.TempDir(), "127.0.0.1:8080", 54321)
	application, _, _ := sentences(cfg)
	_, err := application.Plan(pkit.Deployment{
		Environment: pkit.Development,
		Config:      cfg,
		Inputs:      deploymentInputs(cfg),
		Transports:  transports(),
		Caches:      caches(),
	})
	if err != nil {
		t.Fatalf("the start command's own configuration cannot compose its application: %v", err)
	}
}
