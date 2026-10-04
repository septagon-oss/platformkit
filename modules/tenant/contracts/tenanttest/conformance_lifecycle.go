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

// lifecycleCases are the five verbs the control plane was missing, written as the
// same executable specification the rest of this file is: they run against the fake
// here and against the real service over a real Postgres, so the two floors, the
// released slug and the pair of audit rows are one answer rather than two
// implementations drifting.
//
// Every case starts from a fixture that has an installation tenant
// (Fixture.Operator), because a lifecycle command that cannot write the operator's
// audit row writes nothing at all — see the last case.
func lifecycleCases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"reactivate resumes a suspended tenant": func(t *testing.T, f Fixture) {
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
		},

		"reactivating an active tenant says nothing": func(t *testing.T, f Fixture) {
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
		},

		"rename changes the name and nothing else": func(t *testing.T, f Fixture) {
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
		},

		// The name is the one field of a tenant a body may hold, so these are the
		// refusals a form reads back — and the reason a name with somewhere to hide
		// is refused rather than trimmed is that the body said one thing and the row
		// would say another.
		"a rename that is not a name is refused and changes nothing": func(t *testing.T, f Fixture) {
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
		},

		"renaming a tenant to the name it has says nothing": func(t *testing.T, f Fixture) {
			created, err := f.Service.Create(f.Ctx, f.Tx, acme())
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if _, err := f.Service.Rename(f.Ctx, f.Tx, created.ID, contracts.Rename{Name: created.Name}); err != nil {
				t.Fatalf("Rename to the name it has: %v", err)
			}
			published(t, f, audited(contracts.EventCreated)...)
		},

		"remove-host stops serving one name": func(t *testing.T, f Fixture) {
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
		},

		// The two floors: refuse the write that takes the last one away, never the
		// write that finds none. Each refusal names the verb that would lift it,
		// because an immutable refusal that does not say what would change the world
		// is a dead end rather than an answer.
		"remove-host refuses a tenant's last host": func(t *testing.T, f Fixture) {
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
		},

		"remove-host refuses the primary host": func(t *testing.T, f Fixture) {
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
		},

		"removing a host the tenant does not answer at says nothing": func(t *testing.T, f Fixture) {
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
		},

		"delete needs the slug repeated": func(t *testing.T, f Fixture) {
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
		},

		// The slug is released by the partial unique index of migrations/000006 — the
		// one reason deleted_at is a column rather than a third status, and the
		// reason a delete is reversible in the only sense the control plane cares
		// about: the customer's rows are where they were. The hosts are released by
		// the verb itself, and the case below is why: `tenant_hosts.host` is a global
		// key and a retired tenant cannot be allowed to reserve a name it does not
		// serve.
		"delete releases the slug and keeps the rows": func(t *testing.T, f Fixture) {
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
		},

		// The other half of the release, and the half a routing table cannot leave
		// out. `tenant_hosts.host` is the platform's key for "which customer is this
		// request for?", so a retired tenant that kept its rows would hold every
		// hostname it ever had forever: `RemoveHost` reaches a tenant through the
		// same `deleted_at` filter every read uses, and `AddHost` for the name would
		// be a conflict against a row no reader can find. What the delete keeps is
		// what only the customer read — its rows and its languages — and the pairing
		// of a retired customer with its names lives in `tenant.deleted` from here on.
		"delete releases the tenant's hosts as well as its slug": func(t *testing.T, f Fixture) {
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
		},

		"a deleted tenant is not reactivated, renamed, suspended or given a host": func(t *testing.T, f Fixture) {
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
		},

		"suspending, renaming and reactivating twice each say nothing": func(t *testing.T, f Fixture) {
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
		},

		// The floor, and the case to read first: the installation's own tenant is the
		// door every one of these verbs is walked through, and a verb that closed it
		// could not be undone through it.
		"the installation's own tenant cannot be suspended or deleted": func(t *testing.T, f Fixture) {
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
		},

		// The brief's requirement 2 as one case: the verb's own row in the customer's
		// scope and the installation's mirror beside it, both naming the one request
		// that caused them. This is the case that fails if the second row is ever
		// written with the wrong scope, or with no trace id.
		"one verb writes both audit rows with one trace id": func(t *testing.T, f Fixture) {
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
		},
	}
}

// acmeOperator is the installation's own tenant, which every fixture creates and no
// case is about. The name is used by the fixture harnesses, not by a case.
var acmeOperator = contracts.NewTenant{
	Slug: "installation", Name: "This installation", Host: "ops.example.com", Operator: true,
}
