package app

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"

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

// TestTheDenialEventIsDeclaredWithThePayloadItWrites: KernelEvents is half of how
// security.denied reaches the trail; the declaration in kernelModule is the other
// half, and it is what makes the outbox check the body and the AsyncAPI document
// describe it. Without it the kernel emits an event its own catalogue knows nothing
// about: every denial would be written unchecked, and event_schema_coverage would
// count an event it never looked at.
//
// The two assertions are the promise and its bite — a payload of the type
// recordDenial marshals is accepted, and one member short of that type is refused.
func TestTheDenialEventIsDeclaredWithThePayloadItWrites(t *testing.T) {
	var found *events.Declared
	for _, d := range declaredEvents(withKernel(nil)) {
		if d.Name == EventDenied {
			declared := d
			found = &declared
		}
	}
	if found == nil {
		t.Fatalf("no manifest declares %q, so nothing checks its payload and the document omits it", EventDenied)
	}
	schema := found.Schema()
	if schema == nil {
		t.Fatalf("%s declares no payload type", EventDenied)
	}
	body, err := json.Marshal(Denied{
		Status: 403, Code: "AUTH_DENIED", Detail: "the caller holds no grant",
		Method: "PATCH", Path: "/api/v1/app/task/1", Operation: "task:update",
		RequestID: "01J", UserID: uuid.New(),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := schema.Validate(body); err != nil {
		t.Errorf("the outbox refused the payload recordDenial writes: %v", err)
	}
	if err := schema.Validate([]byte(`{"status":403,"code":"AUTH_DENIED","detail":"x","method":"PATCH","path":"/p"}`)); err == nil {
		t.Error("the schema accepted a denial with no userId, which is the member that makes it attributable")
	}
}
