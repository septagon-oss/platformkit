package module

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/events"
)

// TestTheFillNeverRenamesASubscriptionTheManifestAlreadyNamed holds the fill to
// its promise in the shapes a manifest actually arrives in: the name a manifest
// writes is the durable consumer name and the deliver group, and Validate
// refuses a disagreement, so a fill that renamed one would boot a composition
// whose consumer belongs to a module nobody named. A SubscribeAll template's
// name travels into every subscription the expansion builds, so that shape is
// asked beside both orders of a mixed list.
func TestTheFillNeverRenamesASubscriptionTheManifestAlreadyNamed(t *testing.T) {
	manifests := []struct {
		name string
		mod  Module
	}{
		{"blank entry first", Module{Name: "task", Events: []string{"task.task.created", "task.task.closed"},
			Subscriptions: []events.Subscription{
				{Name: "task.task.created", Handler: handler},
				{Module: "billing", Name: "task.task.closed", Handler: handler},
			}}},
		{"every entry named", Module{Name: "task", Events: []string{"task.task.created"},
			Subscriptions: []events.Subscription{{Module: "billing", Name: "task.task.created", Handler: handler}}}},
		{"SubscribeAll template", Module{Name: "task", Events: []string{"task.task.created"},
			SubscribeAll:  true,
			Subscriptions: []events.Subscription{{Module: "billing", Handler: handler}}}},
	}
	for _, tc := range manifests {
		t.Run(tc.name, func(t *testing.T) {
			written := make([]string, len(tc.mod.Subscriptions))
			for i, s := range tc.mod.Subscriptions {
				written[i] = s.Module
			}
			filled := Expand([]Module{tc.mod})
			kept := false
			for _, s := range filled[0].Subscriptions {
				if s.Module == "billing" {
					kept = true
				} else if s.Module != "task" {
					t.Errorf("the expansion attributed a subscription to %q, which neither name arrived as", s.Module)
				}
			}
			if !kept {
				t.Fatal("the expansion renamed the subscription the manifest named for another module")
			}
			err := Validate(Expand([]Module{tc.mod}))
			if err == nil || !strings.Contains(err.Error(), `is attributed to module "billing"`) {
				t.Fatalf("a manifest naming another module was not refused naming it: %v", err)
			}
			for i, s := range tc.mod.Subscriptions {
				if s.Module != written[i] {
					t.Errorf("Expand wrote the manifest it was given: entry %d went from %q to %q", i, written[i], s.Module)
				}
			}
		})
	}
}
