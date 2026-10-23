package tenanttest

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

func createTenant(t *testing.T, f Fixture) {
	got, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Slug != "acme" || got.Status != contracts.StatusActive {
		t.Errorf("created %q/%q, want acme/active", got.Slug, got.Status)
	}
	if !slices.Contains(got.Hosts, "acme.example.com") {
		t.Errorf("hosts are %v, want the one it was created with", got.Hosts)
	}
	resolved, err := f.Service.ByHost(f.Ctx, f.Tx, "acme.example.com")
	if err != nil {
		t.Fatalf("ByHost: %v", err)
	}
	if resolved.ID != got.ID {
		t.Errorf("ByHost resolved %s, want %s", resolved.ID, got.ID)
	}
	published(t, f, audited(contracts.EventCreated)...)
}

func normaliseTenant(t *testing.T, f Fixture) {
	in := acme()
	in.Slug, in.Host = "  ACME  ", "Acme.Example.COM"
	got, err := f.Service.Create(f.Ctx, f.Tx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Slug != "acme" || !slices.Contains(got.Hosts, "acme.example.com") {
		t.Errorf("stored %q and %v; a host is compared against a normalised Host header", got.Slug, got.Hosts)
	}
}

func refuseInvalidSlug(t *testing.T, f Fixture) {
	for _, bad := range []string{"", "-acme", "acme corp", "acme_corp"} {
		in := acme()
		in.Slug = bad
		if _, err := f.Service.Create(f.Ctx, f.Tx, in); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("Create with slug %q = %v, want ErrInvalid", bad, err)
		}
	}
}

func requireHost(t *testing.T, f Fixture) {
	in := acme()
	in.Host = ""
	if _, err := f.Service.Create(f.Ctx, f.Tx, in); !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("Create with no host = %v, want ErrInvalid: a tenant nothing routes to cannot be signed into", err)
	}
}

func reserveSlug(t *testing.T, f Fixture) {
	if _, err := f.Service.Create(f.Ctx, f.Tx, acme()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	same := contracts.NewTenant{Slug: "acme", Name: "Someone Else", Host: "other.example.com"}
	_, err := f.Service.Create(f.Ctx, f.Tx, same)
	if !errors.Is(err, crud.ErrConflict) {
		t.Errorf("a second tenant with the same slug = %v, want ErrConflict", err)
	}
	if _, ok := errors.AsType[*crud.UniqueConflict](err); !ok {
		t.Errorf("the duplicate slug lost its unique conflict identity: %v", err)
	}
}

func reserveHost(t *testing.T, f Fixture) {
	if _, err := f.Service.Create(f.Ctx, f.Tx, acme()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	host := contracts.NewTenant{Slug: "globex", Name: "Globex", Host: "acme.example.com"}
	_, err := f.Service.Create(f.Ctx, f.Tx, host)
	if !errors.Is(err, crud.ErrConflict) {
		t.Errorf("a second tenant at the same host = %v, want ErrConflict", err)
	}
	if _, ok := errors.AsType[*crud.UniqueConflict](err); !ok {
		t.Errorf("the duplicate host lost its unique conflict identity: %v", err)
	}
}

func resolveUnknownHost(t *testing.T, f Fixture) {
	if _, err := f.Service.ByHost(f.Ctx, f.Tx, "nowhere.example.com"); !errors.Is(err, tenancy.ErrNoSuchHost) {
		t.Errorf("ByHost of an unknown host = %v, want ErrNoSuchHost", err)
	}
}

func addHost(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Service.AddHost(f.Ctx, f.Tx, created.ID, "www.acme.example.com", false); err != nil {
		t.Fatalf("AddHost: %v", err)
	}
	for _, host := range []string{"acme.example.com", "www.acme.example.com"} {
		got, err := f.Service.ByHost(f.Ctx, f.Tx, host)
		if err != nil || got.ID != created.ID {
			t.Errorf("ByHost(%q) = %v, %v", host, got, err)
		}
	}
	// The same host again is the same tenant and no second event.
	if _, err := f.Service.AddHost(f.Ctx, f.Tx, created.ID, "www.acme.example.com", false); err != nil {
		t.Fatalf("AddHost again: %v", err)
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventHostAdded)...)
}

func selectPrimaryHost(t *testing.T, f Fixture) {
	// "The first host" used to mean whichever sorted first, so adding
	// admin.acme.example.com moved every absolute URL this application
	// builds — a mailed set-password link, a notice somebody clicks
	// from outside — onto a name nobody chose. It is now a column.
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := created.Hosts; len(got) != 1 || got[0] != "acme.example.com" {
		t.Fatalf("a new tenant's hosts are %v, want the one it was created with", got)
	}
	// A name that sorts before it, deliberately: this is the addition
	// that used to steal the answer, silently, from every link the
	// application would build from then on.
	const earlier = "a.acme.example.com"
	after, err := f.Service.AddHost(f.Ctx, f.Tx, created.ID, earlier, false)
	if err != nil {
		t.Fatalf("AddHost: %v", err)
	}
	if got := after.Hosts; len(got) != 2 || got[0] != "acme.example.com" {
		t.Errorf("the hosts are %v; the primary one is first and adding a name does not move it", got)
	}
	// And it moves when somebody asks, which is the other half: one
	// primary per tenant, not two.
	moved, err := f.Service.AddHost(f.Ctx, f.Tx, created.ID, earlier, true)
	if err != nil {
		t.Fatalf("AddHost promoting: %v", err)
	}
	if got := moved.Hosts; len(got) != 2 || got[0] != earlier {
		t.Errorf("the hosts are %v after a promotion, want the promoted one first", got)
	}
	// Promoting a host that is already there is not a second addition, but
	// it is a change: the name every absolute URL for this tenant is built on
	// moved, so the verb says so in both trails.
	published(t, f, audited(contracts.EventCreated, contracts.EventHostAdded,
		contracts.EventHostAdded)...)
	// Asking again for the name that is already primary is the same act, so
	// the second call says nothing: an operator's retry never doubles an audit.
	if _, err := f.Service.AddHost(f.Ctx, f.Tx, created.ID, earlier, true); err != nil {
		t.Fatalf("AddHost promoting the primary again: %v", err)
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventHostAdded,
		contracts.EventHostAdded)...)
}

func refuseUnknownTenant(t *testing.T, f Fixture) {
	id := uuid.New()
	if _, err := f.Service.Get(f.Ctx, f.Tx, id); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("Get of an unknown tenant = %v, want ErrNotFound", err)
	}
	if _, err := f.Service.Suspend(f.Ctx, f.Tx, id); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("Suspend of an unknown tenant = %v, want ErrNotFound", err)
	}
	if _, err := f.Service.AddHost(f.Ctx, f.Tx, id, "x.example.com", false); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("AddHost on an unknown tenant = %v, want ErrNotFound", err)
	}
	if _, err := f.Service.SetOIDC(f.Ctx, f.Tx, id, people()); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("SetOIDC on an unknown tenant = %v, want ErrNotFound: a provider cannot be attached to nobody", err)
	}
	if _, err := f.Service.ClearOIDC(f.Ctx, f.Tx, id); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("ClearOIDC on an unknown tenant = %v, want ErrNotFound", err)
	}
}
