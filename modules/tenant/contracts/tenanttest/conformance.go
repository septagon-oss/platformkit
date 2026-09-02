// Package tenanttest is the conformance suite for contracts.Service, and a fake
// that passes it.
//
// An interface is justified by a passing fake and not by a second production
// implementation (AGENTS.md rule 8). RunService is the specification of the
// control plane written as executable cases; the real service and the fake both
// run it, so "the fake behaves like the real thing" is a test result.
package tenanttest

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// Fixture is one case's world: a Service, the cross-tenant transaction its
// commands take, and a way to see what was published. The transaction is the
// real thing for the real service and the zero value for the fake, which never
// looks at it.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.System]
	Service contracts.Service
	// Published is the names of the events published so far, in order. The
	// fake returns its own slice; the real harness reads the outbox. It is what
	// makes "an idempotent command says nothing" a thing the suite can check,
	// rather than a claim the returned entity cannot support.
	Published func() []string
}

// Harness builds one Fixture and calls run with it. It is written this way
// round because the real fixture is a transaction, and a transaction is a scope
// somebody has to close.
type Harness func(t *testing.T, run func(Fixture))

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	for name, run := range cases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f Fixture) { run(t, f) })
		})
	}
}

func acme() contracts.NewTenant {
	return contracts.NewTenant{Slug: "acme", Name: "Acme Corporation", Host: "acme.example.com"}
}

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"create writes the tenant and its first host": func(t *testing.T, f Fixture) {
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
			published(t, f, contracts.EventCreated)
		},

		"create normalises the slug and the host": func(t *testing.T, f Fixture) {
			in := acme()
			in.Slug, in.Host = "  ACME  ", "Acme.Example.COM"
			got, err := f.Service.Create(f.Ctx, f.Tx, in)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if got.Slug != "acme" || !slices.Contains(got.Hosts, "acme.example.com") {
				t.Errorf("stored %q and %v; a host is compared against a normalised Host header", got.Slug, got.Hosts)
			}
		},

		"create refuses a slug that is not a DNS label": func(t *testing.T, f Fixture) {
			for _, bad := range []string{"", "-acme", "acme corp", "acme_corp"} {
				in := acme()
				in.Slug = bad
				if _, err := f.Service.Create(f.Ctx, f.Tx, in); !errors.Is(err, crud.ErrInvalid) {
					t.Errorf("Create with slug %q = %v, want ErrInvalid", bad, err)
				}
			}
		},

		"create refuses a tenant with no host": func(t *testing.T, f Fixture) {
			in := acme()
			in.Host = ""
			if _, err := f.Service.Create(f.Ctx, f.Tx, in); !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("Create with no host = %v, want ErrInvalid: a tenant nothing routes to cannot be signed into", err)
			}
		},

		// The two conflicts are two cases and not one, because they cannot
		// share a transaction: a failed write aborts the whole transaction in
		// Postgres, so a caller that means to try something else after a
		// constraint violation needs a new one — which for an HTTP handler
		// means a new request. See kit/crud.Update.
		"a slug is taken once": func(t *testing.T, f Fixture) {
			if _, err := f.Service.Create(f.Ctx, f.Tx, acme()); err != nil {
				t.Fatalf("Create: %v", err)
			}
			same := contracts.NewTenant{Slug: "acme", Name: "Someone Else", Host: "other.example.com"}
			if _, err := f.Service.Create(f.Ctx, f.Tx, same); !errors.Is(err, crud.ErrConflict) {
				t.Errorf("a second tenant with the same slug = %v, want ErrConflict", err)
			}
		},

		"a host is served by one tenant": func(t *testing.T, f Fixture) {
			if _, err := f.Service.Create(f.Ctx, f.Tx, acme()); err != nil {
				t.Fatalf("Create: %v", err)
			}
			host := contracts.NewTenant{Slug: "globex", Name: "Globex", Host: "acme.example.com"}
			if _, err := f.Service.Create(f.Ctx, f.Tx, host); !errors.Is(err, crud.ErrConflict) {
				t.Errorf("a second tenant at the same host = %v, want ErrConflict", err)
			}
		},

		"an unserved host resolves to nothing": func(t *testing.T, f Fixture) {
			if _, err := f.Service.ByHost(f.Ctx, f.Tx, "nowhere.example.com"); !errors.Is(err, tenancy.ErrNoSuchHost) {
				t.Errorf("ByHost of an unknown host = %v, want ErrNoSuchHost", err)
			}
		},

		"a suspended tenant stops being served": func(t *testing.T, f Fixture) {
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
			all, err := f.Service.List(f.Ctx, f.Tx)
			if err != nil || len(all) != 1 {
				t.Fatalf("List = %d tenants, %v; want the suspended one", len(all), err)
			}
			active, err := contracts.Active{Service: f.Service}.List(f.Ctx, f.Tx)
			if err != nil || len(active) != 0 {
				t.Errorf("Active.List = %v, %v; a suspended tenant is not swept", active, err)
			}
			published(t, f, contracts.EventCreated, contracts.EventSuspended)
		},

		"suspending twice says nothing the second time": func(t *testing.T, f Fixture) {
			created, err := f.Service.Create(f.Ctx, f.Tx, acme())
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			for range 2 {
				if _, err := f.Service.Suspend(f.Ctx, f.Tx, created.ID); err != nil {
					t.Fatalf("Suspend: %v", err)
				}
			}
			published(t, f, contracts.EventCreated, contracts.EventSuspended)
		},

		"another host resolves to the same tenant": func(t *testing.T, f Fixture) {
			created, err := f.Service.Create(f.Ctx, f.Tx, acme())
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if _, err := f.Service.AddHost(f.Ctx, f.Tx, created.ID, "www.acme.example.com"); err != nil {
				t.Fatalf("AddHost: %v", err)
			}
			for _, host := range []string{"acme.example.com", "www.acme.example.com"} {
				got, err := f.Service.ByHost(f.Ctx, f.Tx, host)
				if err != nil || got.ID != created.ID {
					t.Errorf("ByHost(%q) = %v, %v", host, got, err)
				}
			}
			// The same host again is the same tenant and no second event.
			if _, err := f.Service.AddHost(f.Ctx, f.Tx, created.ID, "www.acme.example.com"); err != nil {
				t.Fatalf("AddHost again: %v", err)
			}
			published(t, f, contracts.EventCreated, contracts.EventHostAdded)
		},

		"an unknown tenant is not found": func(t *testing.T, f Fixture) {
			id := uuid.New()
			if _, err := f.Service.Get(f.Ctx, f.Tx, id); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("Get of an unknown tenant = %v, want ErrNotFound", err)
			}
			if _, err := f.Service.Suspend(f.Ctx, f.Tx, id); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("Suspend of an unknown tenant = %v, want ErrNotFound", err)
			}
			if _, err := f.Service.AddHost(f.Ctx, f.Tx, id, "x.example.com"); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("AddHost on an unknown tenant = %v, want ErrNotFound", err)
			}
		},
	}
}

// published asserts the exact sequence of events, which is how an idempotent
// command is held to saying nothing the second time.
func published(t *testing.T, f Fixture, want ...string) {
	t.Helper()
	if f.Published == nil {
		return
	}
	if got := f.Published(); !slices.Equal(got, want) {
		t.Errorf("published %v, want %v", got, want)
	}
}
