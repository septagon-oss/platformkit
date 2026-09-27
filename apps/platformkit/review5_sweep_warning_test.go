package main

// Review 5's pin. The brief's §5 asks for two things at once: the dead grant
// gone from the operator's administrator, "with the hourly warning stopped".
// Every case on this branch reads the *row*; nothing in the repository runs the
// sweep that prints the warning — `grep -rn "no module defines" --include=*_test.go`
// over modules/auth and apps/platformkit finds that sentence only inside
// refusals quoted from CheckedPermissions, never in a captured log record. The
// symptom the task exists to stop is therefore asserted only by inference: the
// sweep prints what internal.Undeclared finds, the repair empties what
// internal.Undeclared reports, therefore the line stops.
//
// That inference has a premise no case checks. The sweep warns only when the
// auth service holds a non-empty catalogue, and it is handed that catalogue in
// Routes, from the API's Declare — a *third* reading of the composition, beside
// the seeder hook's `module.Grants(mods)` in modules.go and the repair command's
// in roles.go. Several readings of one list is the shape this task's defect had.
// A composition whose seeder wrote a permission its own serving half did not
// declare would seed rows that warn forever, and every row-level case on this
// branch would still pass, because each compares the row against a list
// recomputed from the same file that seeded it.
//
// So this case runs the real warning against the real installation:
//
//  1. a bootstrapped composition warns about nothing — and the silence is not
//     vacuous, because the operator row it warns about nothing in is first
//     asserted to hold tenant:manage and billing:catalog, the two operator
//     permissions this file's composition declares and seeds;
//  2. a dead grant planted in that row IS warned about, which proves the
//     warning path was live (nothing empties the catalogue afterwards) and is
//     the reachability probe for step 3 — it asks for the permission by name,
//     not for anything the repair is supposed to have done;
//  3. after `repair-roles --remove`, the same sweep over the same installation
//     warns about nothing: the brief's "the hourly warning stopped", read off
//     the logger and not off the row. The row is then read too, so the silence
//     cannot be hiding the grant's still being there.
//
// Every assertion states the behaviour the branch claims, so this passes where
// the cures stand and fails, naming the role and the permission, where the three
// readings of the catalogue drift apart.

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	billingcontracts "github.com/septagon-oss/platformkit/modules/billing/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// sweepWarningMessage is internal.Service.warn's line, quoted. Rewording that
// sentence changes what this case can see, so the constant is named here rather
// than buried in a match.
const sweepWarningMessage = "auth: a role names permissions no module defines, so they grant nothing"

func TestTheHourlyWarningHasNothingToSayAboutWhatThisCompositionSeeds(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)

	// The application is booted, not merely composed: Routes is where the auth
	// service is handed the catalogue the kernel read off every manifest, and a
	// sweep run against an un-booted composition is silent for the wrong reason.
	start(t, cfg, c.modules, app.Options{
		Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate,
		Role: app.All, Transport: memory.New(), Log: quiet(),
	})

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	sweep := authSweepJob(t, c.modules)
	logs := captureDefaultLog(t)

	// 1. The fresh installation, with the row named first so that the silence
	// below means "nothing to report" and not "there is no such role".
	acme := bootstrappedTenant(t, c, conn)
	held := rolesOf(t, c, conn, acme)
	for _, want := range []string{tenantcontracts.PermissionTenantManage, billingcontracts.PermissionBillingCatalog} {
		if !slices.Contains(held[authcontracts.RoleAdmin], want) {
			t.Errorf("the bootstrapped operator's administrator does not hold %q: %v — this composition composes the module declaring it, so the seeder wrote a narrower row than its own catalogue",
				want, held[authcontracts.RoleAdmin])
		}
	}
	if first := warnedBy(t, sweep, conn, logs); len(first) != 0 {
		t.Errorf("the sweep of an installation this file composes warns about %v; a composition may not seed a grant its own running catalogue calls undeclared", first)
	}

	// 2. The dead grant, planted by SQL because every door refuses to write one,
	// which is why the rows this task repairs can only be an older seeder's.
	plant(t, conn, acme, authcontracts.RoleAdmin, "ghost:read")
	if second := warnedBy(t, sweep, conn, logs); !slices.Contains(second[authcontracts.RoleAdmin], "ghost:read") {
		t.Fatalf("the sweep did not warn about %q in the operator's administrator: %v — the warning path was not live, so the silence above would have meant nothing",
			"ghost:read", second)
	}

	// 3. The repair, then the same sweep over the same installation.
	if err := repairRoles([]string{"--config", path, "--remove"}); err != nil {
		t.Fatalf("repair-roles --remove: %v", err)
	}
	if third := warnedBy(t, sweep, conn, logs); len(third) != 0 {
		t.Errorf("after repair-roles --remove the sweep still warns about %v; the command exists so that this line stops", third)
	}
	if after := rolesOf(t, c, conn, acme)[authcontracts.RoleAdmin]; slices.Contains(after, "ghost:read") {
		t.Errorf("the sweep went silent while the grant it warns about is still in the row: %v", after)
	}
}

// authSweepJob returns the periodic work the composed auth module declared, so
// this runs the job the worker would run rather than a copy of it.
func authSweepJob(t *testing.T, mods []module.Module) jobs.Job {
	t.Helper()
	for _, m := range mods {
		for _, j := range m.Jobs {
			if j.Name == "auth-sweep" {
				return j
			}
		}
	}
	t.Fatal("the composition declares no auth-sweep job: internal.Sweep is no longer this module's periodic work, and this case has to follow whatever replaced it")
	return jobs.Job{}
}

// warnedBy runs the job once over every tenant and returns what it warned about,
// role name to the permissions it named, from the records that run logged.
func warnedBy(t *testing.T, sweep jobs.Job, conn *db.Conn, logs *defaultLog) map[string][]string {
	t.Helper()
	logs.reset()
	if err := sweep.Run(t.Context(), conn); err != nil {
		t.Fatalf("auth-sweep: %v", err)
	}
	out := map[string][]string{}
	for _, record := range logs.take(sweepWarningMessage) {
		var role string
		var permissions []string
		record.Attrs(func(a slog.Attr) bool {
			switch a.Key {
			case "role":
				role = a.Value.String()
			case "permissions":
				if any, ok := a.Value.Any().([]string); ok {
					permissions = any
				}
			}
			return true
		})
		out[role] = append(out[role], permissions...)
	}
	return out
}

// defaultLog captures the default logger, where slog.WarnContext lands when the
// caller put no logger in the context — and internal.Service.warn puts none
// there. Restored on cleanup, so the next case keeps its own helpers' logger.
type defaultLog struct {
	mu      sync.Mutex
	records []slog.Record
}

func (l *defaultLog) Enabled(context.Context, slog.Level) bool { return true }

func (l *defaultLog) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, r.Clone())
	return nil
}

func (l *defaultLog) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *defaultLog) WithGroup(string) slog.Handler      { return l }
func (l *defaultLog) reset()                             { l.take("__none__") }

// take returns the records carrying that message and drops every record, so each
// run of the job answers for itself. reset is take of a message nothing logs.
func (l *defaultLog) take(message string) []slog.Record {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []slog.Record
	for _, r := range l.records {
		if r.Message == message {
			out = append(out, r)
		}
	}
	l.records = nil
	return out
}

func captureDefaultLog(t *testing.T) *defaultLog {
	t.Helper()
	logs := &defaultLog{}
	previous := slog.Default()
	slog.SetDefault(slog.New(logs))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return logs
}

func bootstrappedTenant(t *testing.T, c composition, conn *db.Conn) tenancy.Tenant {
	t.Helper()
	var acme tenancy.Tenant
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var e error
		acme, e = c.tenants.ByHost(ctx, tx, acmeHost)
		return e
	})
	if err != nil {
		t.Fatalf("read the bootstrapped tenant: %v", err)
	}
	return acme
}

func rolesOf(t *testing.T, c composition, conn *db.Conn, tenant tenancy.Tenant) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := db.Run(httpx.WithConn(tenancy.WithTenant(t.Context(), tenant), conn), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			roles, e := c.auth.Roles(ctx, tx)
			if e != nil {
				return e
			}
			for _, r := range roles {
				out[r.Name] = []string(r.Grants)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("read the roles back: %v", err)
	}
	return out
}

// plant leaves the row an older seeder left. Every door refuses to write a
// permission no module defines, so SQL is how such a row came to exist.
func plant(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, role, permission string) {
	t.Helper()
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec(
			"UPDATE roles SET permissions = array_append(permissions, ?) WHERE tenant_id = ? AND name = ?",
			permission, tenant.ID, role).Error
	})
	if err != nil {
		t.Fatalf("leave the row an older seeder left: %v", err)
	}
}
