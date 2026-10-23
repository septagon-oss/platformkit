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

func reactivateTenant(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Service.Suspend(f.Ctx, f.Tx, created.ID); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	got, err := f.Service.Reactivate(f.Ctx, f.Tx, created.ID)
	if err != nil {
		t.Fatalf("Reactivate: %v", err)
	}
	if got.Status != contracts.StatusActive {
		t.Errorf("status is %q after a reactivate, want %q", got.Status, contracts.StatusActive)
	}
	if _, err := f.Service.ByHost(f.Ctx, f.Tx, "acme.example.com"); err != nil {
		t.Errorf("a reactivated tenant's host = %v, want it served again", err)
	}
	active, err := contracts.Active{Service: f.Service}.List(f.Ctx, f.Tx)
	if err != nil {
		t.Fatalf("Active.List: %v", err)
	}
	if !slices.ContainsFunc(active, func(o tenancy.Tenant) bool { return o.ID == created.ID }) {
		t.Errorf("the jobs walk visits %v, want the reactivated tenant back in it", active)
	}
	published(t, f, audited(contracts.EventCreated,
		contracts.EventSuspended, contracts.EventReactivated)...)
}

func repeatReactivation(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for range 2 {
		if _, err := f.Service.Reactivate(f.Ctx, f.Tx, created.ID); err != nil {
			t.Fatalf("Reactivate: %v", err)
		}
	}
	published(t, f, audited(contracts.EventCreated)...)
}

func renameTenant(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Service.Rename(f.Ctx, f.Tx, created.ID, contracts.Rename{Name: "Acme Industries"}); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	after, err := f.Service.Get(f.Ctx, f.Tx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Name != "Acme Industries" {
		t.Errorf("the tenant is called %q, want the name it was renamed to", after.Name)
	}
	// The rest of the tenant is where it was — the whole claim that a rename
	// is a name and not an edit form. The slug is not a field of Rename at
	// all, which is the compiler's half of this case; this is the half that
	// says the stored one did not move either.
	if after.Slug != created.Slug || after.Status != created.Status ||
		!slices.Equal(after.Hosts, created.Hosts) ||
		after.DefaultLocale != created.DefaultLocale ||
		!slices.Equal(after.Locales, created.Locales) ||
		!after.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("a rename moved more than the name: %q/%q/%v/%q", after.Slug, after.Status, after.Hosts, after.DefaultLocale)
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventRenamed)...)
}

func refuseInvalidName(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, bad := range []string{"", "   ", " Acme ", strings.Repeat("A", 201)} {
		if _, err := f.Service.Rename(f.Ctx, f.Tx, created.ID, contracts.Rename{Name: bad}); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("Rename(%q) = %v, want ErrInvalid", bad, err)
		}
	}
	after, err := f.Service.Get(f.Ctx, f.Tx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Name != created.Name {
		t.Errorf("a refused rename left the tenant named %q", after.Name)
	}
	published(t, f, audited(contracts.EventCreated)...)
}

func repeatRename(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Service.Rename(f.Ctx, f.Tx, created.ID, contracts.Rename{Name: created.Name}); err != nil {
		t.Fatalf("Rename to the name it has: %v", err)
	}
	published(t, f, audited(contracts.EventCreated)...)
}

func repeatLifecycle(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, call := range []func() error{
		func() error { _, err := f.Service.Suspend(f.Ctx, f.Tx, created.ID); return err },
		func() error {
			_, err := f.Service.Rename(f.Ctx, f.Tx, created.ID, contracts.Rename{Name: "Acme Twice"})
			return err
		},
		func() error { _, err := f.Service.Reactivate(f.Ctx, f.Tx, created.ID); return err },
	} {
		// Pressed twice, the way an operator's retry does it: the second
		// answer is the same row and no second event.
		for range 2 {
			if err := call(); err != nil {
				t.Fatalf("retry: %v", err)
			}
		}
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventSuspended,
		contracts.EventRenamed, contracts.EventReactivated)...)
}
