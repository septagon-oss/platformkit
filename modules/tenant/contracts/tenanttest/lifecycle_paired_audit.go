package tenanttest

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

func pairAuditTrace(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Service.Suspend(f.Ctx, f.Tx, created.ID); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	// The two rows this verb wrote are the last two of the case.
	names, scopes, traces := f.Published(), f.PublishedScopes(), f.PublishedTraces()
	if len(names) < 2 || len(names) != len(scopes) || len(names) != len(traces) {
		t.Fatalf("%d names, %d scopes, %d traces: the three are one list", len(names), len(scopes), len(traces))
	}
	last := len(names) - 1
	if names[last] != contracts.EventLifecycleRecorded || names[last-1] != contracts.EventSuspended {
		t.Fatalf("the suspension published %v then %v, want the verb and its mirror", names[last-1], names[last])
	}
	if scopes[last-1] != created.ID {
		t.Errorf("the verb is scoped to %s, want the customer whose state changed", scopes[last-1])
	}
	if scopes[last] != f.Operator {
		t.Errorf("the mirror is scoped to %s, want the installation's own tenant: that is the trail an operator reads", scopes[last])
	}
	if traces[last-1] == "" || traces[last] == "" {
		t.Errorf("the two rows carry %q and %q: a trail row with no trace id cannot be joined to its request",
			traces[last-1], traces[last])
	}
	if traces[last-1] != traces[last] {
		t.Errorf("the two rows carry %q and %q, want the one trace id they join on", traces[last-1], traces[last])
	}
}
