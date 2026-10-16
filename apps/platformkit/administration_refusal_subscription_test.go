package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// TestTheAdministrationRefusalsCanBeSubscribedToWithTheirPayloads checks that the
// two refusal events are offered to webhook endpoints exactly once each, and that
// the committed event document describes each with a payload that requires what
// was attempted and when. The refusal is the only record a refused write
// leaves, so a subscriber that cannot pick it or read its shape loses it.
func TestTheAdministrationRefusalsCanBeSubscribedToWithTheirPayloads(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "asyncapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Channels map[string]struct {
			Messages map[string]struct {
				Payload struct {
					Required []string `json:"required"`
				} `json:"payload"`
			} `json:"messages"`
		} `json:"channels"`
		Operations map[string]struct {
			Action string `json:"action"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{usercontracts.EventAdministrationRefused, authcontracts.EventAdministrationRefused} {
		if n := countOf(webhookCatalogue, name); n != 1 {
			t.Errorf("the webhook catalogue names %s %d times, want once", name, n)
		}
		message, ok := document.Channels[name].Messages[name]
		if !ok {
			t.Errorf("the event document has no %s channel message", name)
			continue
		}
		if len(message.Payload.Required) < 2 || !slices.Contains(message.Payload.Required, "at") {
			t.Errorf("%s payload requires %v, want what was attempted and when", name, message.Payload.Required)
		}
		if document.Operations[name].Action != "send" {
			t.Errorf("%s has no send operation", name)
		}
	}
}

func countOf(names []string, name string) int {
	n := 0
	for _, each := range names {
		if each == name {
			n++
		}
	}
	return n
}
