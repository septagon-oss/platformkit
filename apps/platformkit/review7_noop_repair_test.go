package main

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/module"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// TestARepairOfAnInstallationThatNeedsNoRepairChangesNothing is review 7's pin.
//
// `repair-roles --remove` is what an operator runs after reading the README, and on
// an installation no older seeder ever touched it must be a no-op: every roles row
// byte-identical, nothing refused, and the operator's own administrator still
// holding the wildcard that makes it an administrator. Every other case that runs
// the command first writes a dead grant in with raw SQL, and its assertions are all
// about that grant, so the safe run — the one an operator does on a hunch, on an
// installation that turns out to need nothing — was unrun.
//
// What it holds, measured this round: with internal.Undeclared's knowledge of the
// wildcard dropped (`known[contracts.Wildcard] = true` commented out), this case
// fails on the refusal its run now gets —
//
//	repair-roles --remove on an installation with nothing to repair: tenant acme:
//	crud: invalid: "admin" is the last role that grants role:manage
//
// because the repair reads '*' as a departed permission and would empty the row.
// Three other cases also fail on that mutation (the hourly-warning case names the
// row outright), so this is a second lock and not the only one: what it covers alone
// is the no-dead-grant installation, where the other three never get past the row
// they wrote for themselves.
//
// The probe reads the rows a correct seeder leaves, both tenants asked by name, so a
// walk that reached neither, a hook that seeded nothing and an operator catalogue
// that arrived empty all fail there rather than passing on emptiness.
func TestARepairOfAnInstallationThatNeedsNoRepairChangesNothing(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		_, e := c.tenants.Create(ctx, tx, tenantcontracts.NewTenant{
			Slug: "globex", Name: "Globex", Host: globexHost,
		})
		return e
	})
	if err != nil {
		t.Fatalf("create a customer tenant through the composition's hook: %v", err)
	}

	// every role of either tenant, one line per row, read through the system
	// transaction the command's own walk could not reach past
	held := func() map[string]authcontracts.Permissions {
		t.Helper()
		out := map[string]authcontracts.Permissions{}
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			rows, e := tx.DB().Raw(`SELECT t.slug, r.name, array_to_string(r.permissions, ',')
				FROM roles r JOIN tenants t ON t.id = r.tenant_id ORDER BY t.slug, r.name`).Rows()
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var slug, name, joined string
				if e := rows.Scan(&slug, &name, &joined); e != nil {
					return e
				}
				if joined != "" {
					out[slug+"\t"+name] = strings.Split(joined, ",")
				} else {
					out[slug+"\t"+name] = authcontracts.Permissions{}
				}
			}
			return rows.Err()
		})
		if err != nil {
			t.Fatalf("read the seeded roles: %v", err)
		}
		return out
	}

	before := held()
	operator := authcontracts.OperatorGrants(module.Grants(c.modules))
	if len(operator) == 0 {
		t.Fatal("this composition declares no operator permission: nothing here could be stripped")
	}
	admin := before["acme\t"+authcontracts.RoleAdmin]
	if !slices.Contains(admin, authcontracts.Wildcard) ||
		len(slicesIntersection(admin, operator)) == 0 {
		t.Fatalf("the operator's administrator is seeded %v, want the wildcard and an operator permission: without a row the walk could strip, this case proves nothing", admin)
	}
	customer := before["globex\t"+authcontracts.RoleAdmin]
	if !slices.Contains(customer, authcontracts.Wildcard) || len(slicesIntersection(customer, operator)) != 0 {
		t.Fatalf("the customer's administrator is seeded %v, want the wildcard and no operator permission", customer)
	}

	if err := repairRoles([]string{"--config", path, "--remove"}); err != nil {
		t.Fatalf("repair-roles --remove on an installation with nothing to repair: %v", err)
	}
	if after := held(); !reflect.DeepEqual(before, after) {
		t.Errorf("a repair with nothing to repair changed a roles row: before %v, after %v", before, after)
	} else {
		for row, grants := range after {
			if strings.HasSuffix(row, "\t"+authcontracts.RoleAdmin) &&
				!slices.Contains(grants, authcontracts.Wildcard) {
				t.Errorf("%s lost the wildcard to a repair that had nothing to take", row)
			}
		}
	}
}

func slicesIntersection(a, b []string) []string {
	var out []string
	for _, s := range a {
		if slices.Contains(b, s) {
			out = append(out, s)
		}
	}
	return out
}
