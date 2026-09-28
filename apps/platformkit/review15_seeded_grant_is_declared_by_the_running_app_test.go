package main

// review15_seeded_grant_is_declared_by_the_running_app_test.go pins the one join
// every earlier reading of this delivery left as a reading.
//
// The claim is "a grant is only ever as wide as the composition". Two catalogues
// decide it and they are built in two places: the one the seeder is handed is
// module.Grants over the list apps/platformkit/modules.go composed, and the one
// that refuses a request a role ever grants is module.Grants over what kit/app
// stores, which is module.Expand of that same list (kit/app/app.go buildAPI, and
// httpx.API.Declare). Every case in the repository compares the seeded row with
// the first of them, so the second is only ever asserted by the fact that the
// composition happens not to differ — nothing runs it. If Expand ever carried a
// permission the manifests do not (a subscription that implies a permission, a
// generated module), the seeded administrator would go on holding a grant the
// running installation answers 422 for, which is exactly the state the hourly
// warning exists to report, and no comparison of the row with the seeder's own
// list could see it.
//
// So this asks the installation itself, through the one door whose whole job is
// "is every permission in this list one this installation defines"
// (PUT /api/v1/auth/roles/{name}, validated against httpx.Surfaces.Permissions,
// which is the catalogue api.Declare installed): the administrator's own list,
// written back to its own name unchanged. A write of what the row already holds
// changes no row and publishes nothing, so the question costs the installation
// nothing and the answer is either 200 or a refusal naming a permission this
// composition seeded. The assertion is reached through a status code and the
// row's own key — never through anything a broken build prints.

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestEveryGrantTheSeederWroteIsOneTheRunningInstallationDefines(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var acme tenancy.Tenant
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var e error
		acme, e = c.tenants.ByHost(ctx, tx, acmeHost)
		return e
	})
	if err != nil {
		t.Fatalf("read the bootstrapped tenant: %v", err)
	}
	if !acme.Operator {
		t.Fatalf("%s is not the operator's tenant, so no seeded role here names an operator permission", acme.Slug)
	}

	// The row the real bootstrap wrote through the real hook, and the outbox as it
	// stands before this case asks anything of the running installation.
	grants := func() []string {
		var out []string
		err := db.Run(httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				roles, e := c.auth.Roles(ctx, tx)
				if e != nil {
					return e
				}
				for _, r := range roles {
					if r.Name == authcontracts.RoleAdmin {
						out = slices.Clone([]string(r.Grants))
					}
				}
				return nil
			})
		if err != nil {
			t.Fatalf("read %s's roles back: %v", acme.Slug, err)
		}
		if len(out) == 0 {
			t.Fatalf("%s's administrator holds nothing: there is nothing here to ask about", acme.Slug)
		}
		return out
	}
	published := func() int64 {
		var n int64
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE name = ?",
				authcontracts.EventRoleSet).Scan(&n).Error
		})
		if err != nil {
			t.Fatalf("count the role events: %v", err)
		}
		return n
	}
	held := grants()
	before := published()

	// The two catalogues, read where each is built. kit/app declares Expand of the
	// composition to the request guard; the seeder is handed the composition. A
	// permission in one and not the other is the defect this branch was written to
	// end, wearing a new cause, and it is named here by permission rather than by
	// count.
	declared := module.Grants(c.modules)
	if guard := module.Grants(module.Expand(c.modules)); !slices.Equal(declared, guard) {
		t.Errorf("the catalogue the guard validates a role against is not the one the seeder was handed: guard %v, seeder %v",
			guard, declared)
	}
	// Only the operator's own administrator is seeded named permissions at all, so
	// a composition with no operator permission would leave this case comparing
	// two empty lists (see TestTheBootstrapSeedsWhatThisFileComposes for the same
	// guard).
	if len(authcontracts.OperatorGrants(declared)) == 0 {
		t.Fatal("this composition declares no operator permission: the closure in modules.go is the unfilled one")
	}

	start(t, cfg, c.modules, app.Options{
		Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate,
		Role: app.All, Transport: memory.New(), Log: quiet(),
	})
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	permissions := make([]string, 0, len(held))
	for _, p := range held {
		permissions = append(permissions, `"`+p+`"`)
	}
	code, body := do(t, cfg, admin, http.MethodPut, acmeHost,
		"/api/v1/auth/roles/"+authcontracts.RoleAdmin, `{"permissions":[`+strings.Join(permissions, ",")+`]}`)
	// 200 is the whole point: the installation accepts a list of its own seeding.
	// A 422 here says a permission the bootstrap wrote is one the running guard
	// does not define, which is a grant that grants nothing.
	if code != http.StatusOK {
		t.Errorf("writing %s's own seeded grants back to it = %d %s: the installation refuses a grant it seeded itself",
			authcontracts.RoleAdmin, code, body)
	}
	var written struct {
		Name   string   `json:"name"`
		Grants []string `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(body), &written); err != nil {
		t.Fatalf("the role the route answered is not a role: %v\n%s", err, body)
	}
	// The answer names the role it was asked about and what it grants, so the 200
	// above is the installation's own report of the row rather than an empty body.
	if written.Name != authcontracts.RoleAdmin || !slices.Equal(slices.Sorted(slices.Values(written.Grants)), slices.Sorted(slices.Values(held))) {
		t.Errorf("the route answered %q %v, want %q and the seeded grants %v", written.Name, written.Grants, authcontracts.RoleAdmin, held)
	}

	// Asking cost nothing: the row still holds what the bootstrap wrote, and no
	// auth.role_set event exists that did not before. A list that was accepted but
	// rewrote the row would make this case a mutation of the installation rather
	// than a question asked of it.
	if after := grants(); !slices.Equal(held, after) {
		t.Errorf("asking the installation whether it defines %v left the administrator holding %v", held, after)
	}
	if after := published(); after != before {
		t.Errorf("this case published %d new %s events; a write of what a role already holds changes no row and emits nothing",
			after-before, authcontracts.EventRoleSet)
	}
}
