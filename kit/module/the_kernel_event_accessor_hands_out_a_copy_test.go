package module

import (
	"context"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
)

// The kernel names three events itself, and every rule that reads them — the manifest
// gate, the SubscribeAll expansion, the composition's own audit subscription — reads the
// one list. An exported slice is a slice anybody in the program can write into: one
// append that happened to fit in the spare capacity, or one sort of the value a caller
// was handed, would change what every composition in the process emits. The accessor is
// the same read with a copy in between, and these cases are the copy.
func TestTheKernelEventAccessorHandsOutACopy(t *testing.T) {
	names := KernelEventNames()
	if len(names) == 0 {
		t.Fatal("the kernel names no events of its own")
	}
	if !slices.Contains(names, "security.denied") {
		t.Fatalf("KernelEventNames() = %v, want it to list the denial event", names)
	}
	for i := range names {
		names[i] = "tampered.event"
	}
	standing := KernelEventNames()
	if slices.Contains(standing, "tampered.event") {
		t.Fatalf("writing the answer wrote the kernel's list: %v", standing)
	}

	// The rules read the list the kernel holds, not the one a caller was handed: a
	// SubscribeAll module expands into one subscription per kernel event.
	keep := func(context.Context, db.Tx[db.Tenant], events.Event) error { return nil }
	expanded := Expand([]Module{{Name: "trail", SubscribeAll: true,
		Subscriptions: []events.Subscription{{Module: "trail", Handler: keep}}}})
	var subscribed []string
	for _, s := range expanded[0].Subscriptions {
		subscribed = append(subscribed, s.Name)
	}
	for _, want := range standing {
		if !slices.Contains(subscribed, want) {
			t.Errorf("a SubscribeAll module no longer subscribes to the kernel's %s: %v", want, subscribed)
		}
	}
	if err := Validate([]Module{{Name: KernelName, Declared: []events.Declared{{Name: standing[0]}}}}); err != nil {
		t.Errorf("the kernel manifest no longer may declare its own event: %v", err)
	}
}
