package tenanttest

import (
	"errors"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

func suspendTenant(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := f.Service.Suspend(f.Ctx, f.Tx, created.ID)
	if err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	if got.Status != contracts.StatusSuspended {
		t.Errorf("status is %q, want %q", got.Status, contracts.StatusSuspended)
	}
	if _, err := f.Service.ByHost(f.Ctx, f.Tx, "acme.example.com"); !errors.Is(err, tenancy.ErrNoSuchHost) {
		t.Errorf("a suspended tenant's host = %v, want ErrNoSuchHost: from outside it is the same fact", err)
	}
	// Still in the control plane's own list, which is the point of
	// there being two questions.
	// Counted against the fixture's own installation tenant: the control
	// plane's list is every customer there is, and the case is about the one
	// it just suspended.
	all, err := f.Service.List(f.Ctx, f.Tx)
	if err != nil || len(others(all, f.Operator)) != 1 || !slices.ContainsFunc(others(all, f.Operator),
		func(got *contracts.Tenant) bool { return got.Status == contracts.StatusSuspended }) {
		t.Fatalf("List = %v, %v; want the suspended tenant still in it", names(all), err)
	}
	active, err := contracts.Active{Service: f.Service}.List(f.Ctx, f.Tx)
	if err != nil || len(othersTenants(active, f.Operator)) != 0 {
		t.Errorf("Active.List = %v, %v; a suspended tenant is not swept", active, err)
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventSuspended)...)
}

func repeatSuspension(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for range 2 {
		if _, err := f.Service.Suspend(f.Ctx, f.Tx, created.ID); err != nil {
			t.Fatalf("Suspend: %v", err)
		}
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventSuspended)...)
}
