package app

import (
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
)

// TestTheDenialEventIsAKernelEventTheAuditTrailIsSubscribedTo: EventDenied is emitted by
// no module, so it reaches modules/audit only because module.KernelEvents lists it and
// Expand subscribes a SubscribeAll module to every kernel event. The first version of this
// change published it and the trail never saw it; this is the case that says so.
func TestTheDenialEventIsAKernelEventTheAuditTrailIsSubscribedTo(t *testing.T) {
	if !slices.Contains(module.KernelEvents, EventDenied) {
		t.Fatalf("module.KernelEvents %v does not list %q", module.KernelEvents, EventDenied)
	}
	if !events.ValidName(EventDenied) {
		t.Fatalf("%q is not a valid event name", EventDenied)
	}
	trail := module.Module{Name: "trail", SubscribeAll: true,
		Subscriptions: []events.Subscription{{Module: "trail", Handler: nil}}}
	expanded := module.Expand([]module.Module{trail})
	var names []string
	for _, s := range expanded[0].Subscriptions {
		names = append(names, s.Name)
	}
	if !slices.Contains(names, EventDenied) {
		t.Errorf("a SubscribeAll module is subscribed to %v, which does not include %q", names, EventDenied)
	}
}
