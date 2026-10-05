package main

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/pkit"
)

// Every module the reference composition builds from another module's service
// names that service in the edge table, so taking the provider out of the app is
// a refusal naming the contract — not a plan that builds with a value nobody
// composed. The site case has its own test; this one asks the same of every
// provider the composition holds.
func TestRemovingAnyProviderFromTheReferenceCompositionIsRefused(t *testing.T) {
	cfg := referenceDependencyConfig(t)
	if _, err := sentencesOf(cfg).Plan(pkit.Deployment{Environment: pkit.Development, Config: cfg}); err != nil {
		t.Fatalf("the whole reference composition must plan before a provider is removed: %v", err)
	}
	for provider, contract := range map[string]string{
		"user":         "usercontracts.Service",
		"tenant":       "tenantcontracts.Service",
		"notification": "notificationcontracts.Service",
		"auth":         "authcontracts.Auth",
		"content":      "contentcontracts.Service",
		"site":         "sitecontracts.Service",
	} {
		c := compose(cfg)
		kept := c.modules[:0:0]
		removed := false
		for _, m := range c.modules {
			if m.Name == provider {
				removed = true
				continue
			}
			kept = append(kept, m)
		}
		if !removed {
			t.Errorf("the reference composition does not build %s, so its removal was not tested", provider)
			continue
		}
		c.modules = kept
		_, err := sentencesOf(cfg).Plan(pkit.Deployment{Environment: pkit.Development, Config: cfg})
		if err == nil || !strings.Contains(err.Error(), contract) {
			t.Errorf("removing %s.Module must be refused naming %s; Plan returned %v", provider, contract, err)
		}
	}
}
