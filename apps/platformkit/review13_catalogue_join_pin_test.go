package main

// Review 13's pin. One join carries this whole change and no type can see it:
// the catalogue repairRoles reads must be the catalogue the seeding hook was
// given. A narrower list at the command is not inert — SeededGrants takes every
// dead name out of the operator's administrator once it holds the wildcard — so
// this round measured it: `module.Grants(c.modules)` in roles.go changed to
// `module.Grants(nil)` leaves every seeded-vs-composed case green and strips
// `tenant:manage` and `billing:catalog` from acme's administrator. Hence the two
// permissions, named, read off the row after the command ran with --remove (review7_noop_repair_test.go:113 states the same fact as a before/after map).

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestTheRepairCommandCannotTakeALiveOperatorGrant(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	lines, err := printedLines(t, func() error {
		return repairRoles([]string{"--config", path, "--remove"})
	})
	if err != nil {
		t.Fatalf("repair-roles --remove: %v\n%v", err, lines)
	}
	for _, line := range lines {
		if strings.Contains(line, "removed") {
			t.Errorf("a run over an installation this file composed reported a removal: %v", lines)
			break
		}
	}
	var grants []string
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var joined string
		if e := tx.DB().Raw(`SELECT array_to_string(r.permissions, ',') FROM roles r
			JOIN tenants t ON t.id = r.tenant_id WHERE t.slug = 'acme' AND r.name = 'admin'`).
			Row().Scan(&joined); e != nil {
			return e
		}
		grants = strings.Split(joined, ",")
		return nil
	})
	if err != nil {
		t.Fatalf("read acme's administrator: %v", err)
	}
	for _, live := range []string{"tenant:manage", "billing:catalog"} {
		if !slices.Contains(grants, live) {
			t.Errorf("the repair took %q from acme's administrator, which a composed module defines: %v",
				live, grants)
		}
	}
}
