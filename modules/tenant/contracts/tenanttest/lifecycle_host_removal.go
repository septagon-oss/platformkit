package tenanttest

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

func removeHost(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	const extra = "www.acme.example.com"
	if _, err := f.Service.AddHost(f.Ctx, f.Tx, created.ID, extra, false); err != nil {
		t.Fatalf("AddHost: %v", err)
	}
	got, err := f.Service.RemoveHost(f.Ctx, f.Tx, created.ID, extra)
	if err != nil {
		t.Fatalf("RemoveHost: %v", err)
	}
	if want := []string{"acme.example.com"}; !slices.Equal(got.Hosts, want) {
		t.Errorf("the hosts are %v after the removal, want %v", got.Hosts, want)
	}
	if _, err := f.Service.ByHost(f.Ctx, f.Tx, extra); !errors.Is(err, tenancy.ErrNoSuchHost) {
		t.Errorf("the removed host = %v, want ErrNoSuchHost", err)
	}
	if _, err := f.Service.ByHost(f.Ctx, f.Tx, "acme.example.com"); err != nil {
		t.Errorf("the primary host = %v, want it still served", err)
	}
	published(t, f, audited(contracts.EventCreated,
		contracts.EventHostAdded, contracts.EventHostRemoved)...)
}

func preserveLastHost(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = f.Service.RemoveHost(f.Ctx, f.Tx, created.ID, "acme.example.com")
	if !errors.Is(err, crud.ErrConflict) {
		t.Errorf("removing the only host = %v, want ErrConflict: a tenant nobody routes to cannot be signed into", err)
	}
	if _, err := f.Service.ByHost(f.Ctx, f.Tx, "acme.example.com"); err != nil {
		t.Errorf("the refused removal took the host anyway: %v", err)
	}
	published(t, f, audited(contracts.EventCreated)...)
}

func preservePrimaryHost(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	const extra = "www.acme.example.com"
	if _, err := f.Service.AddHost(f.Ctx, f.Tx, created.ID, extra, false); err != nil {
		t.Fatalf("AddHost: %v", err)
	}
	_, err = f.Service.RemoveHost(f.Ctx, f.Tx, created.ID, "acme.example.com")
	if !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("removing the primary host = %v, want ErrConflict", err)
	}
	if !strings.Contains(err.Error(), "add-host") {
		t.Errorf("the refusal does not name the verb that lifts it: %v", err)
	}
	// Promote the other name and the same removal goes through: the floor is
	// a tenant with no primary, not this particular host.
	if _, err := f.Service.AddHost(f.Ctx, f.Tx, created.ID, extra, true); err != nil {
		t.Fatalf("AddHost promoting: %v", err)
	}
	if _, err := f.Service.RemoveHost(f.Ctx, f.Tx, created.ID, "acme.example.com"); err != nil {
		t.Errorf("removing the old primary after a promotion = %v, want it allowed", err)
	}
}

func removeAbsentHost(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Service.AddHost(f.Ctx, f.Tx, created.ID, "www.acme.example.com", false); err != nil {
		t.Fatalf("AddHost: %v", err)
	}
	for range 2 {
		if _, err := f.Service.RemoveHost(f.Ctx, f.Tx, created.ID, "someone-else.example.com"); err != nil {
			t.Fatalf("RemoveHost of a host that is not this tenant's = %v, want the no-op that leaks nothing", err)
		}
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventHostAdded)...)
}
