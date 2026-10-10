// migrations/000043_tenant_app.up.sql places the tenants that already exist under
// the app the boot declares — and the placing is the part that has to be proved. A
// file that wrote the boot's slug into every row would be a guess dressed as a
// migration: it would decide, from one process's configuration, that every tenant
// in the database was that process's, which is the write that cannot be undone
// (no route rewrites the column, and a tenant moved to the wrong app takes its rows
// with it).
//
// So the case runs the file the way a release reaches it: an installation at the
// version before, two tenants already in the table — one served at a host this boot
// serves, one at a host it does not — and then the migration, three times.
//
//   - declared nothing: refused, naming both tenants, and the schema left as found;
//   - declared its slug and its hosts: the provable tenant would be placed and the
//     other one is not, so the run as a whole still refuses — a partial placement is
//     exactly how a tenant ends up under an app that is not its own;
//   - declared the operator's mapping over the top: every row is then placed by
//     something somebody declared, and the file applies.
package migrations_test

import (
	"context"
	"io/fs"
	"strconv"
	"strings"
	"testing"

	"database/sql"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
)

// appFile is the file under test, by the name the ledger carries.
const appFile = "000043_tenant_app.up.sql"

// appVersion is that file's ledger number, read off its own name so the two cannot
// drift: every file from here on postdates the installation this fixture plants.
var appVersion, _ = fileVersion(appFile)

// beforeApp is the kernel's own history with that one file — and every file
// appended after it — taken out: the state of an installation on the day the
// release that carries it boots.
//
// The files after it belong in the removal for the same reason 000043 does, and
// leaving them in refuses this fixture for a reason it does not ask about: an
// installation that predates the release predates everything appended since, so a
// fixture that applied 000046 first would have the ledger read 46, and kit/db would
// answer the second run below with "000043 precedes applied version 46; append a new
// version" — an ordering refusal, reached before the back-fill, and every assertion
// here would be reading the wrong error. The higher files are absent from the
// fixture rather than skipped after the fact: the point of the run is the state of
// an installation, not a list of names.
type beforeApp struct{ inner fs.FS }

func (b beforeApp) Open(name string) (fs.File, error) { return b.inner.Open(name) }

func (b beforeApp) ReadDir(name string) ([]fs.DirEntry, error) {
	rows, err := fs.ReadDir(b.inner, name)
	if err != nil {
		return nil, err
	}
	out := rows[:0:0]
	for _, row := range rows {
		if version, numbered := fileVersion(row.Name()); numbered && version >= appVersion {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// fileVersion is the ledger number a migration file carries, and whether it carries
// one at all. The number is the part before the first underscore — the same split
// kit/db and every ledger count in this repository make — and it is read as a number
// because the four digits are a filename convention, not the ledger: 000043 and 43
// name one file to kit/db, and a fixture that compared text would place 1000043
// below 21.
func fileVersion(name string) (int, bool) {
	if !strings.HasSuffix(name, ".up.sql") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
	return n, err == nil
}

func TestTheTenantAppColumnIsPlacedOnProofAndRefusedWithoutIt(t *testing.T) {
	ctx := context.Background()
	adminURL, _ := dbtest.URLs(t)
	// The source as migrations/embed.go declares it, with one file withheld — floor
	// and all, because the floor is that source's own declaration about bytes that
	// already ran somewhere and is not this test's to raise or lower.
	prior := db.MigrationSource{
		Owner:     migrations.Source.Owner,
		Files:     beforeApp{migrations.Source.Files},
		RulesFrom: migrations.Source.RulesFrom,
	}
	if err := db.Migrate(ctx, adminURL, prior); err != nil {
		t.Fatalf("the installation at the version before: %v", err)
	}
	// One tenant is served at the host the boot will declare, the other somewhere
	// else. Both are in the table before the column exists, which is the difficulty.
	withAdmin(t, ctx, adminURL, func(admin *sql.DB) {
		plant(t, ctx, admin, "acme-provable", "acme.example.com")
		plant(t, ctx, admin, "elsewhere-at", "other.example.com")
	})

	// One: a boot that declares nothing, with tenants to place. Refused, naming
	// both, and the schema left exactly as it was found.
	err := db.Migrate(ctx, adminURL, migrations.Source)
	if err == nil {
		t.Fatal("the back-fill accepted a boot that declared nothing, with two tenants to place")
	}
	for _, want := range []string{"acme-provable", "elsewhere-at", "<nothing>"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not name %s", err, want)
		}
	}
	if state := placed(t, ctx, adminURL); state != noColumn {
		t.Errorf("after the refusal the column is %q; the file's own transaction left it behind", state)
	}

	// Two: the slug and the hosts. The tenant whose every host is on the list is
	// provable; the one with a host outside it is somebody else's, and until an
	// operator says where it goes the whole run refuses.
	slug := "acme"
	declared := db.Declaration{App: &slug, Hosts: []string{"acme.example.com"}}
	if err := db.MigrateDeclaring(ctx, adminURL, db.MigrationBudget{}, declared, migrations.Source); err == nil {
		t.Fatal("the back-fill placed every tenant on a host list that covers only one of them")
	} else if strings.Contains(err.Error(), "acme-provable") {
		t.Errorf("the refusal %q still names the tenant the hosts do prove", err)
	}
	if state := placed(t, ctx, adminURL); state != noColumn {
		t.Errorf("after the second refusal the column is %q; nothing this run did may survive it", state)
	}

	// Three: the operator's mapping over the top. The second tenant now has a
	// declaration behind it rather than an inference, and the file applies.
	declared.Tenants = map[string]string{"elsewhere-at": "shelf"}
	if err := db.MigrateDeclaring(ctx, adminURL, db.MigrationBudget{}, declared, migrations.Source); err != nil {
		t.Fatalf("the placement with a mapping that covers the rest: %v", err)
	}
	got := map[string]string{}
	withAdmin(t, ctx, adminURL, func(admin *sql.DB) {
		rows, err := admin.QueryContext(ctx, `SELECT slug, app FROM tenants ORDER BY slug`)
		if err != nil {
			t.Fatalf("read the placement back: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var tenant, app string
			if err := rows.Scan(&tenant, &app); err != nil {
				t.Fatal(err)
			}
			got[tenant] = app
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	})
	if got["acme-provable"] != "acme" {
		t.Errorf("the tenant on the declared host was placed under %q, want the app that declares it", got["acme-provable"])
	}
	if got["elsewhere-at"] != "shelf" {
		t.Errorf("the tenant the operator mapped was placed under %q, want shelf", got["elsewhere-at"])
	}
	var nullable string
	withAdmin(t, ctx, adminURL, func(admin *sql.DB) {
		if err := admin.QueryRowContext(ctx,
			`SELECT is_nullable FROM information_schema.columns
			  WHERE table_schema = current_schema()
				AND table_name = 'tenants' AND column_name = 'app'`).Scan(&nullable); err != nil {
			t.Fatalf("the column's nullability: %v", err)
		}
	})
	if nullable != "NO" {
		t.Errorf("tenants.app is nullable=%s after a placement that placed everything", nullable)
	}
	// One operator per app is the other half of the same file: two apps, two
	// operators, one database, and the index that used to be unique per database.
	withAdmin(t, ctx, adminURL, func(admin *sql.DB) {
		if _, err := admin.ExecContext(ctx,
			`INSERT INTO tenants (slug, name, app, operator) VALUES
				('ops-acme', 'Ops Acme', 'acme', true), ('ops-shelf', 'Ops Shelf', 'shelf', true)`); err != nil {
			t.Errorf("one operator per app: %v", err)
		}
		if _, err := admin.ExecContext(ctx,
			`INSERT INTO tenants (slug, name, app, operator) VALUES ('ops-acme-two', 'Ops Two', 'acme', true)`); err == nil {
			t.Error("a second operator tenant inside one app was accepted; tenants_operator is unique on (app)")
		}
	})
}

// noColumn is what columnState answers when the column is not there — the shape of
// a run that refused itself rather than half-applying.
const noColumn = "\x00"

// columnState is what tenants.app holds: `slug=app` for every row, or noColumn when
// the migration left no column at all. Reading the catalog first is what keeps the
// two answers apart: an empty string would be the same word for "no rows" and "no
// column", and the difference is the whole claim of the refusal cases.
//
// Every catalog read names current_schema() because dbtest gives each test a schema
// of its own inside one database: unqualified, information_schema answers about
// whichever other test's tenants table it likes, and the case that passes alone and
// fails under a full `make test` is the shape of that mistake.
func placed(t *testing.T, ctx context.Context, url string) string {
	t.Helper()
	var state string
	withAdmin(t, ctx, url, func(admin *sql.DB) { state = columnState(t, ctx, admin) })
	return state
}

// withAdmin opens the owner's handle, uses it, and closes it before returning.
// Nothing else may hold the database open across a migration: the runner opens its
// own pool and closes it when the run ends, and Go's database/sql deadlocks a
// query against a pool that is closing — measured here, in closemu.go, with one
// owner handle left open over two migrations and the second never coming back.
func withAdmin(t *testing.T, ctx context.Context, url string, use func(*sql.DB)) {
	t.Helper()
	admin := dbtest.OpenFor(t, url)
	defer func() { _ = admin.Close() }()
	use(admin)
}

func columnState(t *testing.T, ctx context.Context, admin *sql.DB) string {
	t.Helper()
	var columns int
	if err := admin.QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.columns
		  WHERE table_schema = current_schema()
			AND table_name = 'tenants' AND column_name = 'app'`).Scan(&columns); err != nil {
		t.Fatalf("asked the catalog for tenants.app: %v", err)
	}
	if columns == 0 {
		return noColumn
	}
	var state string
	if err := admin.QueryRowContext(ctx,
		`SELECT coalesce(string_agg(slug || '=' || app, ',' ORDER BY slug), '<no rows>') FROM tenants`).
		Scan(&state); err != nil {
		t.Fatalf("read the column back: %v", err)
	}
	return state
}

// plant writes a tenant and the one host it is served at, the way an installation
// that predates the column would have them.
func plant(t *testing.T, ctx context.Context, admin *sql.DB, slug, host string) {
	t.Helper()
	var id string
	if _, err := admin.ExecContext(ctx,
		`INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $2)`, uuid.New(), slug); err != nil {
		t.Fatalf("a tenant before the column: %v", err)
	}
	if err := admin.QueryRowContext(ctx, `SELECT id FROM tenants WHERE slug = $1`, slug).Scan(&id); err != nil {
		t.Fatalf("read the tenant back: %v", err)
	}
	if _, err := admin.ExecContext(ctx,
		`INSERT INTO tenant_hosts (host, tenant_id, is_primary) VALUES ($1, $2, true)`, host, id); err != nil {
		t.Fatalf("a host before the column: %v", err)
	}
}
