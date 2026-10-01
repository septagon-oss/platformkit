// migrations/000031_durable_app.up.sql moves the two delivery ledgers when a durable
// changes its name — and the moving is the part that has to be proved, because the cost
// of a rename that leaves rows behind is invisible until it is paid: `DeliverAll`
// redelivers what the transport still holds, the claim is keyed by (event_id, durable),
// and a row under the old key answers nothing under the new one, so every handler that
// already ran runs again and the ledger that exists to stop that says nothing. The same
// new name is what kit/events/replay.go deletes by for a targeted replay, so a dead
// letter left under the old one is invisible to the one command that would read it.
//
// The case is the shape the rename actually arrives in: an installation at the version
// before, rows already written under the unscoped name by tenants of *two* apps and by a
// third tenant whose installation names no slug at all, and then the file — run by the
// application role rather than the owner.
//
// The role is the point as much as the rows are. The runner sets
// `platformkit.system_access` on a system transaction and on a data drain and around a
// schema file never, and both ledgers and `tenants` are RLS-protected, so at the migrate
// role a deployment really names — the owner, no superuser, no BYPASSRLS
// (kit/db/dbtest) — a file that writes without saying it is the control plane finds
// nothing to write and records its version over the silence. The assertions below are
// unreadable unless that write writes.
//
// Three things the file has to answer, one per tenant: a tenant of app `acme` moves to
// `acme+…`, a tenant of app `academy` moves to `academy+…` in the same pass, and a tenant
// whose app is the empty slug does not move at all, because `appname.Durable` with no
// slug set answers the unscoped name and the row already carries the key its subscription
// will ask for. The fourth case is the rolling window: one event handled under both
// spellings is one event that ran, so the scoped row stands and the unscoped twin goes.
package migrations_test

import (
	"context"
	"io/fs"
	"testing"

	"database/sql"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
)

// durableAppFile is the file under test, by the name the ledger carries.
const durableAppFile = "000031_durable_app.up.sql"

// beforeDurableApp is the kernel's history with that file taken out: an installation on
// the day the release that carries the rename boots.
type beforeDurableApp struct{ inner fs.FS }

func (b beforeDurableApp) Open(name string) (fs.File, error) { return b.inner.Open(name) }

func (b beforeDurableApp) ReadDir(name string) ([]fs.DirEntry, error) {
	rows, err := fs.ReadDir(b.inner, name)
	if err != nil {
		return nil, err
	}
	out := rows[:0:0]
	for _, row := range rows {
		if row.Name() != durableAppFile {
			out = append(out, row)
		}
	}
	return out, nil
}

// ownTheLedgers gives the application role the tables the file writes, which is what
// "migrate_url names the owner, which holds the DDL rights" describes in a deployment
// that does not hand its schema to a superuser. dbtest's role is NOSUPERUSER
// NOBYPASSRLS, so under the two tables' own FORCE ROW LEVEL SECURITY this is the role at
// which a write that names no system access silently writes nothing.
func ownTheLedgers(t *testing.T, ctx context.Context, admin *sql.DB, appURL string, tables ...string) {
	t.Helper()
	role := dbtest.RoleOf(t, appURL)
	for _, table := range tables {
		if _, err := admin.ExecContext(ctx, "ALTER TABLE "+table+" OWNER TO "+role); err != nil {
			t.Fatalf("making the migration role the owner of %s: %v", table, err)
		}
	}
}

// ledgerRow is one claim or one dead letter as the test plants it: which tenant, and
// which module's which event. The durable is never spelled here: it is whatever
// appname.Durable forms, because the migration and the runtime that reads the ledger
// have to agree on that string and a hand-written copy in the test could agree with
// nobody. The old spelling is the same constructor with no slug set, which is exactly
// what an installation that named no app wrote.
type ledgerRow struct {
	tenant string
	module string
	event  string
}

// unscoped is the durable a build before the app segment wrote into the ledger.
func unscoped(module, event string) string { return appname.Durable("", module, event) }

// scoped is the durable appname.Durable forms now, and the only name a subscription of
// that app will ever ask the ledger for again.
func scoped(app, module, event string) string {
	return appname.Durable(appname.MustParse(app), module, event)
}

// wantName is the durable a row of this ledger is expected to carry once the file ran:
// the app-scoped one for a tenant of an app, the unscoped one for a tenant of an
// installation that names no slug.
func wantName(row ledgerRow) string {
	switch row.tenant {
	case "acme-tenant":
		return scoped("acme", row.module, row.event)
	case "academy-tenant":
		return scoped("academy", row.module, row.event)
	default:
		return unscoped(row.module, row.event)
	}
}

func TestTheHandledLedgerAndTheDeadLettersMoveWithTheirDurables(t *testing.T) {
	ctx := context.Background()
	adminURL, appURL := dbtest.URLs(t)
	prior := db.MigrationSource{
		Owner:     migrations.Source.Owner,
		Files:     beforeDurableApp{migrations.Source.Files},
		RulesFrom: migrations.Source.RulesFrom,
	}
	if err := db.Migrate(ctx, adminURL, prior); err != nil {
		t.Fatalf("the installation before the durable rename: %v", err)
	}

	file, err := fs.ReadFile(migrations.Source.Files, durableAppFile)
	if err != nil {
		t.Fatalf("reading %s: %v", durableAppFile, err)
	}

	handled := []ledgerRow{
		{tenant: "acme-tenant", module: "billing", event: "plan.created"},
		{tenant: "acme-tenant", module: "task", event: "task.updated"},
		{tenant: "academy-tenant", module: "billing", event: "plan.created"},
		{tenant: "unnamed-tenant", module: "billing", event: "plan.created"},
	}
	dead := []ledgerRow{
		{tenant: "acme-tenant", module: "billing", event: "plan.failed"},
		{tenant: "academy-tenant", module: "billing", event: "plan.failed"},
		{tenant: "unnamed-tenant", module: "billing", event: "plan.failed"},
	}
	// The rolling window's collision: one event of app acme, claimed twice, once by a
	// pod that named no app and once by a pod that did. Both rows exist; the event ran
	// once. The file has to read it that way, or its own UPDATE raises a duplicate key
	// on the row this deployment is standing on.
	collision := uuid.New()

	handledIDs := map[uuid.UUID]ledgerRow{}
	deadIDs := map[uuid.UUID]ledgerRow{}
	unnamedHandled := uuid.New()

	withAdmin(t, ctx, adminURL, func(admin *sql.DB) {
		apps := map[string]string{
			"acme-tenant": "acme", "academy-tenant": "academy", "unnamed-tenant": "",
		}
		ids := map[string]uuid.UUID{}
		for slug, app := range apps {
			plant(t, ctx, admin, slug, slug+".example.com")
			var id uuid.UUID
			if err := admin.QueryRowContext(ctx, `SELECT id FROM tenants WHERE slug = $1`, slug).Scan(&id); err != nil {
				t.Fatalf("read the tenant back: %v", err)
			}
			if _, err := admin.ExecContext(ctx, `UPDATE tenants SET app = $2 WHERE id = $1`, id, app); err != nil {
				t.Fatalf("giving %s its app: %v", slug, err)
			}
			ids[slug] = id
		}
		claim := func(table, event string, rows []ledgerRow, into map[uuid.UUID]ledgerRow, one uuid.UUID, window bool) {
			for _, r := range rows {
				id := uuid.New()
				if window && r.tenant == "acme-tenant" && r.event == "plan.created" {
					id = one
				}
				durable := unscoped(r.module, r.event)
				stmt := `INSERT INTO ` + table + ` (event_id, durable, tenant_id) VALUES ($1, $2, $3)`
				if table == "platformkit_dead_letters" {
					stmt = `INSERT INTO ` + table + `
					  (event_id, durable, tenant_id, name, error) VALUES ($1, $2, $3, $4, $5)`
				}
				args := []any{id, durable, ids[r.tenant]}
				if table == "platformkit_dead_letters" {
					args = append(args, event, "the handler refused it the third time as it refuses it now")
				}
				if _, err := admin.ExecContext(ctx, stmt, args...); err != nil {
					t.Fatalf("planting %s (%s, %s) in %s: %v", durable, r.tenant, id, table, err)
				}
				into[id] = r
			}
		}
		claim("platformkit_handled", "billing.plan.created", handled, handledIDs, collision, true)
		claim("platformkit_dead_letters", "billing.plan.failed", dead, deadIDs, uuid.Nil, false)
		if _, err := admin.ExecContext(ctx,
			`INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES ($1, $2, $3)`,
			collision, scoped("acme", "billing", "plan.created"), ids["acme-tenant"]); err != nil {
			t.Fatalf("planting the collision's scoped twin: %v", err)
		}
		if _, err := admin.ExecContext(ctx,
			`INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES ($1, $2, $3)`,
			unnamedHandled, unscoped("billing", "plan.created"), ids["unnamed-tenant"]); err != nil {
			t.Fatalf("planting a second row for the unnamed tenant: %v", err)
		}
		t.Cleanup(func() {
			for _, table := range []string{"platformkit_handled", "platformkit_dead_letters"} {
				_, _ = admin.ExecContext(ctx, `DELETE FROM `+table+` WHERE tenant_id IN (SELECT id FROM tenants WHERE slug LIKE '%-tenant')`)
			}
			_, _ = admin.ExecContext(ctx, `DELETE FROM tenant_hosts WHERE tenant_id IN (SELECT id FROM tenants WHERE slug LIKE '%-tenant')`)
			_, _ = admin.ExecContext(ctx, `DELETE FROM tenants WHERE slug LIKE '%-tenant'`)
		})

		// The file, run whole, by the role that is the tables' owner and is neither
		// superuser nor BYPASSRLS — which is what config.example.yaml says the migrate
		// door is held by, and the only role at which FORCE ROW LEVEL SECURITY bites a
		// file that writes without naming itself the control plane. The shape is
		// review_r8_locale_backfill_role_test.go's: make the role the owner, then run.
		run := func(who string) {
			t.Helper()
			app := dbtest.Open(t, appURL)
			if _, err := app.ExecContext(ctx, string(file)); err != nil {
				t.Fatalf("%s could not run %s: %v", who, durableAppFile, err)
			}
		}
		ownTheLedgers(t, ctx, admin, appURL, "platformkit_handled", "platformkit_dead_letters")
		run("the role that owns these tables and holds no system access")

		want := func(table string, id uuid.UUID, row ledgerRow) string {
			t.Helper()
			var durables string
			if err := admin.QueryRowContext(ctx,
				`SELECT coalesce(string_agg(durable, ',' ORDER BY durable), '<none>') FROM `+table+`
				  WHERE event_id = $1`, id).Scan(&durables); err != nil {
				t.Fatalf("reading %s's rows in %s: %v", id, table, err)
			}
			return durables
		}
		for id, row := range handledIDs {
			if got, expected := want("platformkit_handled", id, row), wantName(row); got != expected {
				t.Errorf("the handled claim of %s at %s reads %q, want the durable this app's subscription asks for, %q",
					row.tenant, unscoped(row.module, row.event), got, expected)
			}
		}
		for id, row := range deadIDs {
			if got, expected := want("platformkit_dead_letters", id, row), wantName(row); got != expected {
				t.Errorf("the dead letter of %s at %s reads %q, want %q: a targeted replay deletes by the exact durable, so a row left under the old name is invisible to the command that would replay it",
					row.tenant, unscoped(row.module, row.event), got, expected)
			}
		}
		if got := want("platformkit_handled", collision, ledgerRow{}); got != scoped("acme", "billing", "plan.created") {
			t.Errorf("the event the window handled under both spellings reads %q: one event handled twice is one event that ran, so the scoped row stands and the unscoped twin goes", got)
		}
		if got := want("platformkit_handled", unnamedHandled, ledgerRow{}); got != unscoped("billing", "plan.created") {
			t.Errorf("an installation that names no slug had its handled row moved to %q: its durable did not change, so the move is a rename of a key its subscription will never ask for", got)
		}

		// And the second run, which is the same release applied to a database that has
		// nothing left to move: every row already carries a '+'.
		run("the same role, once more")
		for id, row := range handledIDs {
			if got := want("platformkit_handled", id, row); wantName(row) != got {
				t.Errorf("the second run moved the handled claim of %s again: it reads %q", row.tenant, got)
			}
		}
	})
}
