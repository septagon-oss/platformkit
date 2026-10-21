package main

import (
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/pkit"
)

// A file-borne window longer than the day the counter keeps is the plan's
// refusal, naming the key, before anything opens; a large quota plans.
func TestTheCompositionRefusesAnAccountRequestWindowTheCounterCannotKeep(t *testing.T) {
	cfg := referenceCompositionConfig(t)
	window := 48 * time.Hour
	cfg.Auth.AccountRequests.Window = &window
	_, err := sentencesOf(cfg).Plan(pkit.Deployment{Environment: pkit.Development, Config: cfg})
	if err == nil || !strings.Contains(err.Error(), "auth.account_requests") || !strings.Contains(err.Error(), "24h") {
		t.Fatalf("the plan answered %v, want the window refused under auth.account_requests against the counter's 24h", err)
	}

	cfg = referenceCompositionConfig(t)
	quota := 1000
	cfg.Auth.AccountRequests.Quota = &quota
	if _, err := sentencesOf(cfg).Plan(pkit.Deployment{Environment: pkit.Development, Config: cfg}); err != nil {
		t.Fatalf("a quota of 1000 does not plan: %v", err)
	}
}
