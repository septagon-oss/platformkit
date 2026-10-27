package main

// This file asks the one question no committed case asks.
//
// Every case in this branch decides "is this grant dead" from a list and then
// reads a row back to check what the command did with that answer. The list is
// the composition's catalogue and the row is in the same database, so the two
// halves of that agree by construction: nothing in the suite ever asks the
// installation. The installation is the party that honours a permission — the
// guard every request passes answers "does this deployment define this
// permission", and it was declared from kit/app's own copy of that catalogue
// (kit/app buildAPI, httpx.API.Declare). So the sentence this delivery carries —
// a repair takes away only grants that grant nothing — has an independent oracle
// and never consults it.
//
// This case consults it, on both sides, and on the one row where the repair is
// allowed to take a grant the seeder never wrote. acme is this file's own
// composition's operator tenant, so its administrator is seeded the wildcard and
// the operator permissions of the manifests. Two grants are then planted in that
// row by hand, the way the roles screen would have: "task:read", which a composed
// module defines and a live route answers to, and "ghost:read", which nothing
// defines. The command run against that row must take one and leave the other,
// and the assertion that it left the right one is made twice — once off the row,
// and once by the running installation, which accepts a write naming what it
// defines and refuses one naming "ghost:read". The second half is what makes the
// case about the invariant rather than about one list's opinion: a repair that
// reported taking a permission the installation goes on serving would leave the
// row looking repaired and the person without the grant they were told they kept.
//
// The reachability probe is the removal itself: ghost:read is gone from the row
// and named by the printed line, so the run cannot pass by doing nothing. Every
// assertion is a row, a count, a permission name or a status code — none is a
// sentence only a broken command prints.

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
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestNoGrantTheRepairPrintsIsOneTheRunningInstallationStillHonours(t *testing.T) {
	const dead, live = "ghost:read", "task:read"

	path, cfg := configureFresh(t)
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
		acme, e = c.testTenants().ByHost(ctx, tx, acmeHost)
		return e
	})
	if err != nil {
		t.Fatalf("read the bootstrapped tenant: %v", err)
	}
	if !acme.Operator {
		t.Fatalf("%s is not the operator's tenant, so its administrator is not the row the repair may over-take", acme.Slug)
	}

	// Which of the two planted grants is dead is decided here from the manifests
	// this file composes — a second derivation, not the closure under test — so a
	// composition that dropped the task module fails this line instead of quietly
	// asserting that the repair took both.
	catalogue := module.Grants(c.modules)
	if !slices.ContainsFunc(catalogue, func(g tenancy.Grant) bool { return g.Permission == live }) {
		t.Fatalf("%q is not in the composition's catalogue (%v): this case needs a live permission beside the dead one", live, catalogue)
	}
	if slices.ContainsFunc(catalogue, func(g tenancy.Grant) bool { return g.Permission == dead }) {
		t.Fatalf("%q is defined by a composed module: nothing here would be a dead grant to take", dead)
	}

	row := func() []string {
		var out []string
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			var roles []authcontracts.Role
			if e := tx.DB().Where("tenant_id = ? AND name = ?", acme.ID, authcontracts.RoleAdmin).Find(&roles).Error; e != nil {
				return e
			}
			if len(roles) != 1 {
				t.Errorf("the operator tenant holds %d administrator rows, want one", len(roles))
				return nil
			}
			out = slices.Clone([]string(roles[0].Grants))
			return nil
		})
		if err != nil {
			t.Fatalf("read %s's administrator: %v", acme.Slug, err)
		}
		return out
	}
	events := func() int {
		return countRows(t, conn, "platformkit_outbox WHERE name = '"+authcontracts.EventRoleSet+"'")
	}

	held := row()
	if !slices.Contains(held, authcontracts.Wildcard) {
		t.Fatalf("%s's administrator holds %v, want the wildcard: this is the row whose dead grants the repair takes whole", acme.Slug, held)
	}
	if !slices.Contains(held, "billing:catalog") {
		t.Fatalf("%s's administrator holds %v: with no seeded operator permission in the row, the run below could report a removal it invented", acme.Slug, held)
	}
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		for _, p := range []string{dead, live} {
			if e := tx.DB().Exec(
				"UPDATE roles SET permissions = array_append(permissions, ?) WHERE tenant_id = ? AND name = ?",
				p, acme.ID, authcontracts.RoleAdmin).Error; e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("plant %q and %q by hand: %v", dead, live, err)
	}

	before := events()
	lines, err := printedLines(t, func() error { return repairRoles([]string{"--config", path, "--remove"}) })
	if err != nil {
		t.Fatalf("repair-roles --remove: %v\n%v", err, lines)
	}
	removals := []string{}
	for _, line := range lines {
		if strings.Contains(line, "\tremoved\t") {
			removals = append(removals, line)
		}
	}
	if len(removals) != 1 {
		t.Fatalf("the run printed %d removal lines (%v), want one: %v", len(removals), removals, lines)
	}
	if !strings.Contains(removals[0], acme.Slug) || !strings.Contains(removals[0], authcontracts.RoleAdmin) ||
		!strings.Contains(removals[0], dead) {
		t.Errorf("the removal line is %q, want it to name %s, %s and %q", removals[0], acme.Slug, authcontracts.RoleAdmin, dead)
	}
	// The line claiming a removal of a live permission would name it by the same
	// key the row uses, so this counts what the run claimed about the live one.
	if claims := slices.ContainsFunc(removals, func(l string) bool { return strings.Contains(l, live) }); claims {
		t.Errorf("the run claimed it removed %q from a live installation: %v", live, removals)
	}

	after := row()
	if slices.Contains(after, dead) {
		t.Errorf("the operator's administrator still holds %q, which no composed module defines: %v", dead, after)
	}
	for _, keep := range []string{authcontracts.Wildcard, live, "billing:catalog", "tenant:manage"} {
		if !slices.Contains(after, keep) {
			t.Errorf("the repair took %q from %s's administrator, which this composition defines and the wildcard alone does not grant: %v",
				keep, acme.Slug, after)
		}
	}
	if n := events(); n != before+1 {
		t.Errorf("the repair published %d %s events, want exactly one", n-before, authcontracts.EventRoleSet)
	}

	// The installation's own verdict, asked through the one door whose job is
	// "is every permission in this list one this deployment defines". Two answers
	// are needed: the surviving list accepted, and the printed one refused. A run
	// that refused everything would satisfy the second alone.
	start(t, cfg, c.modules, app.Options{
		Tenants: c.testTenants(), Authorize: c.testAuth(), Entitle: c.plans, Authenticate: c.testAuth().Authenticate,
		Role: app.All, Transport: memory.New(), Log: quiet(),
	})
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	quoted := make([]string, 0, len(after))
	for _, p := range after {
		quoted = append(quoted, `"`+p+`"`)
	}
	code, body := do(t, cfg, admin, http.MethodPut, acmeHost, "/api/v1/auth/roles/"+authcontracts.RoleAdmin,
		`{"permissions":[`+strings.Join(quoted, ",")+`]}`)
	if code != http.StatusOK {
		t.Errorf("writing the surviving grants %v back = %d %s: the installation refuses a grant it keeps on serving", after, code, body)
	}
	var written struct {
		Name   string   `json:"name"`
		Grants []string `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(body), &written); err != nil {
		t.Fatalf("the role the route answered is not a role: %v\n%s", err, body)
	}
	if written.Name != authcontracts.RoleAdmin || !slices.Equal(slices.Sorted(slices.Values(written.Grants)),
		slices.Sorted(slices.Values(after))) {
		t.Errorf("the route answered %q %v, want %q and %v", written.Name, written.Grants, authcontracts.RoleAdmin, after)
	}
	code, body = do(t, cfg, admin, http.MethodPut, acmeHost, "/api/v1/auth/roles/"+authcontracts.RoleAdmin,
		`{"permissions":["`+authcontracts.Wildcard+`","`+dead+`"]}`)
	if code < 400 {
		t.Errorf("writing %q to a role = %d %s: the repair reported removing a grant the running installation accepts, which is a grant that still grants something",
			dead, code, body)
	}
	settled := row()
	if slices.Contains(settled, dead) {
		t.Errorf("the refusal wrote %q back into %s's administrator: %v", dead, acme.Slug, settled)
	}
	if !slices.Contains(settled, live) {
		t.Errorf("the refused write took %q, a grant this composition defines, from %s's administrator: %v", live, acme.Slug, settled)
	}
	if n := events(); n != before+1 {
		t.Errorf("asking the installation answered with %d %s events beyond the repair's own one", n-before-1, authcontracts.EventRoleSet)
	}
}
