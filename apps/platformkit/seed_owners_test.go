package main

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
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
// a record a file names that is not there yet is created through its owner's write
// path and not inserted; that applying files the tenant already holds writes
// nothing at all, whether they arrived through the creation hook or a command; and
// that a demo request for a tenant whose row says false costs the run.
//
// The person the run acts as is the bootstrap administrator, and the authorizer
// asks the auth module about that person: a run that could not pass the same
// question as a request would not reach this far.
//
// The records this case creates are its own — an fstest tree with two records the
// reference files do not name — because the reference application's own starter
// now exists before any command runs: the tenant creation hook applied it (see
// starter_on_create_test.go), so a run over seed/starter/ proves the reconciliation
// and not the creation. Creation is proven here, against the same owners, with the
// same writers and the same authorizer.
func TestTheReferenceSeedWritesEachRecordThroughItsOwner(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	service, err := seedService(c)
	if err != nil {
		t.Fatalf("the composition's own seed service: %v", err)
	}
	fresh, err := seed.New(seed.Deps{
		Files: fstest.MapFS{
			"seed/starter/contents.yaml": {Data: []byte(`apiVersion: platformkit.seed/v1
resource: contents
records:
  - key: hand-off
    fields: {kind: page, title: "  Hand off  ", body: "Who takes this over?"}
    commands: [{name: publish}]
`)},
			"seed/starter/users.yaml": {Data: []byte(`apiVersion: platformkit.seed/v1
resource: users
records:
  - key: ada@example.test
    fields: {displayName: Ada Lovelace, roles: [observer]}
`)},
		},
		Root: "seed", Clock: seedClock{},
		Writers:   []seed.Writer{&contentSeeder{svc: c.contents}, &userSeeder{users: c.users}},
		Authorize: seedGrants{auth: c.auth},
	})
	if err != nil {
		t.Fatalf("the seed this case writes with: %v", err)
	}
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var created, again, own, ownAgain seed.Plan
	var home, hand *contentcontracts.Content
	var ada *usercontracts.User
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if ctx, err = seedActor(ctx, c.users, tx, adminEmail); err != nil {
				return err
			}
			if created, err = fresh.Apply(ctx, tx, seed.Selection{}); err != nil {
				return err
			}
			if again, err = fresh.Apply(ctx, tx, seed.Selection{}); err != nil {
				return err
			}
			// The application's own files, applied by hand to the tenant the
			// creation hook already seeded: reconciliation over rows this run did
			// not write, through the same keys the hook wrote them under.
			if own, err = service.Apply(ctx, tx, seed.Selection{}); err != nil {
				return err
			}
			if ownAgain, err = service.Apply(ctx, tx, seed.Selection{}); err != nil {
				return err
			}
			// The starter's home declares publish, so the page the module serves to
			// anybody is the seeded row or this fails: an unowned status write could
			// not have moved both status and publication time through the command.
			if home, err = c.contents.Public(ctx, tx, "home"); err != nil {
				return err
			}
			if hand, err = c.contents.Public(ctx, tx, "hand-off"); err != nil {
				return err
			}
			if ada, err = c.users.ByEmail(ctx, tx, "ada@example.test"); err != nil {
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

	if got := counts(created, seed.Create, "starter"); got["contents"] != 1 || got["users"] != 1 {
		t.Errorf("the run created %v; a record that is not there yet goes through its owner", got)
	}
	if got := counts(again, seed.Create, "starter"); got["contents"] != 0 {
		t.Errorf("the second run created %v; an unchanged record costs no write", got)
	}
	changed := 0
	for _, item := range again.Items {
		if item.Action != seed.Unchanged {
			changed++
		}
	}
	if changed != 0 || !strings.Contains(again.String(), "0 created, 0 updated, 2 unchanged") {
		t.Errorf("the second run wrote something: %s", again)
	}
	// The application's own files, a second time, over a tenant that got them from
	// the create hook rather than from a command: the same provenance keys answer
	// both paths, so neither run has anything to write.
	if got := counts(own, seed.Create, "starter"); got["contents"] != 0 || got["sites"] != 0 {
		t.Errorf("the reference starter created %v over a tenant the hook seeded; %s", got, own)
	}
	if got := counts(ownAgain, seed.Unchanged, "starter"); got["contents"] != 2 || got["sites"] != 1 {
		t.Errorf("the reference starter reads back as %v; want its two pages and its site record", got)
	}
	// What the module stored, not what the file typed: the title has lost the
	// spaces around it, and the row is a page because the module said so.
	if hand.Title != "Hand off" || hand.Kind != contentcontracts.KindPage {
		t.Errorf("the seeded page reads back as %+v", hand)
	}
	if home.Title != "Home" || home.Kind != contentcontracts.KindPage {
		t.Errorf("the seeded home reads back as %+v", home)
	}
	if !ada.Roles.Has("observer") {
		t.Errorf("the seeded person holds %v; the file named observer, through user.SetRoles", ada.Roles)
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

// TestTheSeedCommandRunsAgainstItsTenant exercises the subcommand itself, not the
// service behind it: the flags, the configuration, the cross-tenant transaction the
// command opens for itself and the tenant view it takes from that. A dry run of the
// starter answers without error and writes nothing it would not write for real, and
// the same command asked for demo records at a tenant whose row says false comes
// back with the refusal — which is the brief's "even when asked", answered at the
// door an operator stands at rather than at the service.
func TestTheSeedCommandRunsAgainstItsTenant(t *testing.T) {
	path, _ := configure(t)
	install(t, path)
	args := []string{"--config", path, "--tenant", "acme", "--as", adminEmail}
	if err := seedCommand(append([]string{}, append(args, "--dry-run")...)); err != nil {
		t.Errorf("seed --dry-run: %v", err)
	}
	err := seedCommand(append([]string{}, append(args, "--demo")...))
	if err == nil || !strings.Contains(err.Error(), "non-demo tenant") {
		t.Errorf("seed --demo at a tenant whose row says false: %v", err)
	}
	if err := seedCommand([]string{"--config", path, "--tenant", "nosuch", "--as", adminEmail}); err == nil ||
		!strings.Contains(err.Error(), "no tenant nosuch") {
		t.Errorf("seed --tenant nosuch: %v", err)
	}
	if err := seedCommand([]string{"--config", path}); err == nil ||
		!strings.Contains(err.Error(), "--tenant is required") {
		t.Errorf("seed with nothing named: %v", err)
	}
}
