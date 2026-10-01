package main

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// counts is a plan reduced to what a case can assert on: how many records of one
// resource each action took, told apart by the kind directory the file sits in —
// because one run of a demo tenant's files creates the starter's records too, and
// a count that could not tell the two apart could not say which half went missing.
func counts(plan seed.Plan, action seed.Action, kind string) map[string]int {
	out := map[string]int{}
	for _, item := range plan.Items {
		if item.Action == action && strings.HasPrefix(item.Source.File, "seed/"+kind+"/") {
			out[item.Resource]++
		}
	}
	return out
}

// TestTheReferenceSeedWritesEachRecordThroughItsOwner runs the application's own
// seed over the composition it is declared in, against a bootstrapped
// installation. Four things are settled here and nowhere else: that a seeded page
// is a page — created, published and read back through the content module, with
// the slug and title the module stored rather than the ones the file typed; that
// applying the same files again writes nothing; that a demo request for a tenant
// whose row says false costs the run, and that the same files applied to a tenant
// whose row says true invite three people and write five pages.
//
// The person the run acts as is the bootstrap administrator, and the authorizer
// asks the auth module about that person: a run that could not pass the same
// question as a request would not reach this far.
func TestTheReferenceSeedWritesEachRecordThroughItsOwner(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	service, err := seedService(c)
	if err != nil {
		t.Fatalf("the composition's own seed service: %v", err)
	}
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var first, again seed.Plan
	var demoPlan seed.Plan
	var home *contentcontracts.Content
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if ctx, err = seedActor(ctx, c.users, tx, adminEmail); err != nil {
				return err
			}
			if first, err = service.Apply(ctx, tx, seed.Selection{}); err != nil {
				return err
			}
			if again, err = service.Apply(ctx, tx, seed.Selection{}); err != nil {
				return err
			}
			// The starter's home declares publish, so the page the module serves to
			// anybody is the seeded row or this fails: an unowned status write could
			// not have moved both status and publication time through the command.
			if home, err = c.contents.Public(ctx, tx, "home"); err != nil {
				return err
			}
			// Acme's row says false, so demo records are refused for it. The command
			// line cannot override this; the check reads the row, not the request.
			if _, err := service.Apply(ctx, tx, seed.Selection{Demo: true}); err == nil ||
				!strings.Contains(err.Error(), "non-demo tenant") {
				t.Errorf("demo records for a non-demo tenant: %v", err)
			}
			return nil
		})
	}); err != nil {
		t.Fatalf("seed acme: %v", err)
	}

	if got := counts(first, seed.Create, "starter"); got["contents"] != 2 {
		t.Errorf("the first run created %v; want the starter's two pages", got)
	}
	if got := counts(again, seed.Create, "starter"); got["contents"] != 0 {
		t.Errorf("the second run created %v; an unchanged record costs no write", got)
	}
	if again.Items != nil {
		changed := 0
		for _, item := range again.Items {
			if item.Action != seed.Unchanged {
				changed++
			}
		}
		if changed != 0 || !strings.Contains(again.String(), "0 created, 0 updated, 2 unchanged") {
			t.Errorf("the second run wrote something: %s", again)
		}
	}
	if home.Title != "Home" || home.Kind != contentcontracts.KindPage {
		t.Errorf("the seeded home reads back as %+v", home)
	}

	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		created, err := c.tenants.Create(ctx, system, tenantcontracts.NewTenant{
			Slug: "demo", Name: "Demo Walkthrough", Host: "demo.localhost", Demo: true,
		})
		if err != nil {
			return err
		}
		if _, err := c.users.Provision(ctx, system, created.ID, "root@demo.localhost", "Root",
			adminPass, []string{authcontracts.RoleAdmin}); err != nil {
			return err
		}
		return db.InTenant(ctx, system, created.Tenancy(), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if ctx, err = seedActor(ctx, c.users, tx, "root@demo.localhost"); err != nil {
				return err
			}
			demoPlan, err = service.Apply(ctx, tx, seed.Selection{Demo: true})
			return err
		})
	}); err != nil {
		t.Fatalf("seed the demo tenant: %v", err)
	}
	if got := counts(demoPlan, seed.Create, "demo"); got["users"] != 3 || got["contents"] != 5 {
		t.Errorf("the demo records created %v; the brief asks for three people and five pages", got)
	}
	if got := counts(demoPlan, seed.Create, "starter"); got["contents"] != 2 {
		t.Errorf("the demo tenant's starter records created %v; a run always includes the starter", got)
	}
}

// TestTheSeedRefusesARecordItsOwnerWouldRefuse hands the composition's own seed a
// record the content module refuses on its own terms — a title of nothing — and
// asserts the run dies with the module's sentence, at the file's address, having
// written no page at all. The owner's problem text surviving the seed wrapper is
// the whole claim: a seed that reworded its owners would be a seed that hid them.
func TestTheSeedRefusesARecordItsOwnerWouldRefuse(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	sentences := map[string]string{}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if ctx, err = seedActor(ctx, c.users, tx, adminEmail); err != nil {
				return err
			}
			broken, err := seed.New(seed.Deps{
				Files: fstest.MapFS{"seed/starter/contents.yaml": {Data: []byte(
					"apiVersion: platformkit.seed/v1\nresource: contents\nrecords:\n  - key: untitled\n    fields: {title: ' '}\n")}},
				Root: "seed", Clock: seedClock{},
				Writers:   []seed.Writer{&contentSeeder{svc: c.contents}},
				Authorize: seedGrants{auth: c.auth},
			})
			if err != nil {
				return err
			}
			if _, err := broken.Apply(ctx, tx, seed.Selection{}); err != nil {
				sentences["error"] = err.Error()
				_, readErr := c.contents.Public(ctx, tx, "untitled")
				sentences["read"] = readErr.Error()
				return nil
			}
			sentences["error"] = ""
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sentences["error"], "a page needs a title") ||
		!strings.Contains(sentences["error"], "seed/starter/contents.yaml:4") {
		t.Errorf("refusal = %q; want the content module's own sentence at the file's line", sentences["error"])
	}
	if !strings.Contains(sentences["read"], "not found") && !strings.Contains(sentences["read"], "no such") {
		t.Errorf("the refused page reads back as %q; a refused run writes nothing", sentences["read"])
	}
}
