package tenanttest

import (
	"errors"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

func requireDeletionSlug(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Service.Delete(f.Ctx, f.Tx, created.ID, contracts.Delete{Confirm: "globex"}); !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("Delete with the wrong confirm = %v, want ErrInvalid", err)
	}
	if _, err := f.Service.ByHost(f.Ctx, f.Tx, "acme.example.com"); err != nil {
		t.Errorf("the refused delete retired the tenant anyway: %v", err)
	}
	if _, err := f.Service.Delete(f.Ctx, f.Tx, created.ID, contracts.Delete{Confirm: "ACME"}); err != nil {
		t.Fatalf("Delete with the slug: %v", err)
	}
	if _, err := f.Service.Get(f.Ctx, f.Tx, created.ID); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("Get of a deleted tenant = %v, want ErrNotFound: every reader filters deleted_at", err)
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventDeleted)...)
}

func deleteAndRetainRows(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Service.Delete(f.Ctx, f.Tx, created.ID, contracts.Delete{Confirm: "acme"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	again, err := f.Service.Create(f.Ctx, f.Tx, contracts.NewTenant{
		Slug: "acme", Name: "Acme Again", Host: "acme-again.example.com",
	})
	if err != nil {
		t.Fatalf("the released slug could not be taken again: %v", err)
	}
	if again.ID == created.ID {
		t.Error("the second acme is the first one")
	}
	if _, err := f.Service.ByHost(f.Ctx, f.Tx, "acme.example.com"); !errors.Is(err, tenancy.ErrNoSuchHost) {
		t.Errorf("the retired tenant's host = %v, want ErrNoSuchHost: nothing serves a name a delete released", err)
	}
	all, err := f.Service.List(f.Ctx, f.Tx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if slices.ContainsFunc(all, func(o *contracts.Tenant) bool { return o.ID == created.ID }) {
		t.Error("a deleted tenant is in the control plane's list")
	}
}

func releaseDeletedHosts(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Service.AddHost(f.Ctx, f.Tx, created.ID, "www.acme.example.com", false); err != nil {
		t.Fatalf("AddHost: %v", err)
	}
	if _, err := f.Service.Delete(f.Ctx, f.Tx, created.ID, contracts.Delete{Confirm: "acme"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// A retired customer is not found by its own verb either: a retry that
	// answered "already done" would be a reader that can still see it.
	if _, err := f.Service.Delete(f.Ctx, f.Tx, created.ID, contracts.Delete{Confirm: "acme"}); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("deleting a retired tenant again = %v, want ErrNotFound", err)
	}
	successor, err := f.Service.Create(f.Ctx, f.Tx, contracts.NewTenant{
		Slug: "second", Name: "Second Corporation", Host: "acme.example.com",
	})
	if err != nil {
		t.Fatalf("a hostname retired with a tenant is not servable again: %v", err)
	}
	resolved, err := f.Service.ByHost(f.Ctx, f.Tx, "acme.example.com")
	if err != nil {
		t.Fatalf("ByHost acme.example.com: %v", err)
	}
	if resolved.ID != successor.ID {
		t.Errorf("acme.example.com resolves to %s, want the tenant just hosted there (%s)", resolved.ID, successor.ID)
	}
	// The name that was not the primary one is free too, and it is the one a
	// retired tenant's `remove-host` could never have reached.
	if _, err := f.Service.AddHost(f.Ctx, f.Tx, successor.ID, "www.acme.example.com", false); err != nil {
		t.Errorf("the retired tenant's second host is not servable again: %v", err)
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventHostAdded,
		contracts.EventDeleted, contracts.EventCreated, contracts.EventHostAdded)...)
}

func refuseDeletedTenant(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Service.Delete(f.Ctx, f.Tx, created.ID, contracts.Delete{Confirm: "acme"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	calls := map[string]func() error{
		"reactivate": func() error { _, err := f.Service.Reactivate(f.Ctx, f.Tx, created.ID); return err },
		"rename": func() error {
			_, err := f.Service.Rename(f.Ctx, f.Tx, created.ID, contracts.Rename{Name: "Somebody Else"})
			return err
		},
		"suspend": func() error { _, err := f.Service.Suspend(f.Ctx, f.Tx, created.ID); return err },
		"add-host": func() error {
			_, err := f.Service.AddHost(f.Ctx, f.Tx, created.ID, "new.example.com", false)
			return err
		},
		"remove-host": func() error { _, err := f.Service.RemoveHost(f.Ctx, f.Tx, created.ID, "acme.example.com"); return err },
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("%s of a deleted tenant = %v, want ErrNotFound", name, err)
		}
	}
}

func protectOperatorTenant(t *testing.T, f Fixture) {
	if _, err := f.Service.Suspend(f.Ctx, f.Tx, f.Operator); !errors.Is(err, crud.ErrConflict) {
		t.Errorf("Suspend of the operator tenant = %v, want ErrConflict", err)
	}
	_, err := f.Service.Delete(f.Ctx, f.Tx, f.Operator, contracts.Delete{Confirm: "installation"})
	if !errors.Is(err, crud.ErrConflict) {
		t.Errorf("Delete of the operator tenant = %v, want ErrConflict", err)
	}
	got, err := f.Service.Get(f.Ctx, f.Tx, f.Operator)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != contracts.StatusActive {
		t.Errorf("the installation is %q after the refusals, want it still served", got.Status)
	}
	// A rename is no floor: it changes what the installation is called and
	// nothing about whether it can be reached.
	renamed, err := f.Service.Rename(f.Ctx, f.Tx, f.Operator, contracts.Rename{Name: "Northwind Platform"})
	if err != nil {
		t.Fatalf("Rename of the operator tenant: %v", err)
	}
	if renamed.Name != "Northwind Platform" {
		t.Errorf("the installation is named %q", renamed.Name)
	}
	// One event and no mirror: the subject and the installation are the same
	// tenant, and nothing but that branch stops the duplicate row.
	if got := f.Published(); !slices.Equal(got, []string{contracts.EventRenamed}) {
		t.Errorf("renaming the installation published %v, want the one event", got)
	}
}
