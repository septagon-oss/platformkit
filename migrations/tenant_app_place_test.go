// migrations/000046_tenant_app_place.up.sql places the tenants an app-less boot
// stamped with the empty slug — the rows 000043 left behind because it had already
// made the column NOT NULL by the time it ran, and cannot be rewritten. Two cases
// matter, and they are the two halves of one rule.
//
// A tenant neither host nor mapping speaks for is the write that takes the last one
// away if it is guessed (nothing rewrites this column, and a tenant under the wrong
// app takes its rows with it), so a boot that named itself stops and names the
// tenant rather than place the easy rows and call the run a success.
//
// A boot that named nothing asserts nobody's tenant, and stopping it stops the
// release for every deployment that has not taken a slug — `make check`'s rehearsal
// is that upgrade, and it refused (measured 2026-10-05). Such a boot places what an
// operator's mapping names and leaves the rest exactly as it found them, which is
// still served: the empty slug keeps its legacy durable because kit/events' move
// renames nothing for a tenant no app claims.
package migrations_test

import (
	"context"
	"database/sql"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
)

// placedAppFile is the file under test, by the name the ledger carries.
const placedAppFile = "000046_tenant_app_place.up.sql"

func TestTheEmptySlugPlacementNamesTheTenantItCannotPlace(t *testing.T) {
	ctx := context.Background()
	adminURL, _ := dbtest.URLs(t)
	// The history the drain actually starts from: 000043 applied, so the column
	// exists, is NOT NULL and holds '' for every row an app-less boot stamped.
	files := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.Source.Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		version, _, found := strings.Cut(entry.Name(), "_")
		if entry.IsDir() || !found || version > "000043" {
			continue
		}
		body, err := fs.ReadFile(migrations.Source.Files, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = &fstest.MapFile{Data: body, Mode: 0444}
	}
	prior := db.MigrationSource{Owner: migrations.Source.Owner, Files: files, RulesFrom: migrations.Source.RulesFrom}
	if err := db.Migrate(ctx, adminURL, prior); err != nil {
		t.Fatalf("the installation an app-less boot left: %v", err)
	}
	withAdmin(t, ctx, adminURL, func(admin *sql.DB) {
		plant(t, ctx, admin, "shop", "shop.example.com")
		plant(t, ctx, admin, "school", "school.example.com")
		plant(t, ctx, admin, "lost", "lost.example.com")
	})
	// The boot names itself, names one host, and maps one tenant. Two of the three
	// rows have an answer behind them; "lost" has none, and the run refuses rather
	// than place the two it can and record the file as applied.
	slug := "collect"
	decl := db.Declaration{App: &slug, Hosts: []string{"shop.example.com"},
		Tenants: map[string]string{"school": "academy"}}
	err = db.MigrateDeclaring(ctx, adminURL, db.MigrationBudget{}, decl, migrations.Source)
	if err == nil {
		t.Fatalf("%s accepted a tenant neither its hosts nor its mapping speaks for", placedAppFile)
	}
	for _, want := range []string{"lost", "shop.example.com", "school=academy"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not name %s", err, want)
		}
	}
	if state := placed(t, ctx, adminURL); state != "lost=,school=,shop=" {
		t.Errorf("after the refusal the tenants read %q; a refused placement writes nothing", state)
	}

	// The same run, with the operator's answer added. The refusal is correctable
	// rather than fatal: the retry is the same command, and every row lands under the
	// app the declaration for it names — the mapped tenant by its mapping, the tenant
	// on the declared host by the host, and not one of them by a guess.
	decl.Tenants["lost"] = "shelf"
	if err := db.MigrateDeclaring(ctx, adminURL, db.MigrationBudget{}, decl, migrations.Source); err != nil {
		t.Fatalf("the placement with a mapping that covers the rest: %v", err)
	}
	if state := placed(t, ctx, adminURL); state != "lost=shelf,school=academy,shop=collect" {
		t.Errorf("after the placement the tenants read %q; want each under the app its declaration gives it", state)
	}
}

// The rehearsal's own boot: hosts named, no slug. This is the single-app deployment
// that has not joined the two-app vocabulary, and the release has to apply over it.
// It places the tenant the operator's mapping names — both halves of that answer came
// from the operator, so it is nobody's guess — and leaves the tenant whose only claim
// is a host behind, because placing it needs a slug and this boot has none to give.
func TestTheApplessBootPlacesWhatItsMappingNamesAndRefusesNothing(t *testing.T) {
	ctx := context.Background()
	adminURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.Source.Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		version, _, found := strings.Cut(entry.Name(), "_")
		if entry.IsDir() || !found || version > "000043" {
			continue
		}
		body, err := fs.ReadFile(migrations.Source.Files, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = &fstest.MapFile{Data: body, Mode: 0444}
	}
	prior := db.MigrationSource{Owner: migrations.Source.Owner, Files: files, RulesFrom: migrations.Source.RulesFrom}
	if err := db.Migrate(ctx, adminURL, prior); err != nil {
		t.Fatalf("the installation an app-less boot left: %v", err)
	}
	withAdmin(t, ctx, adminURL, func(admin *sql.DB) {
		plant(t, ctx, admin, "shop", "shop.example.com")
		plant(t, ctx, admin, "school", "school.example.com")
	})
	// App stays nil: this is a boot that named itself nothing, which is what
	// `platformkit migrate` declares for a configuration with no nats.app.
	decl := db.Declaration{Hosts: []string{"shop.example.com"}, Tenants: map[string]string{"school": "academy"}}
	if err := db.MigrateDeclaring(ctx, adminURL, db.MigrationBudget{}, decl, migrations.Source); err != nil {
		t.Fatalf("the release refused a boot that named no app: %v", err)
	}
	if state := placed(t, ctx, adminURL); state != "school=academy,shop=" {
		t.Errorf("after an app-less placement the tenants read %q; want the mapped tenant placed and the host-only one left as it was found", state)
	}
}
