package db_test

// review11_a_contract_half_waits_for_a_version_this_release_can_point_at_test.go is the
// eleventh round's own answer to the question its acceptance review wrote and then left
// unpinned on purpose: a contract half beside an expansion the same release runs *after* it
// (contract@2 `expand=3`). The review recorded the case and said which way it is answered is
// "the round's decision, not mine to pin".
//
// The decision is this: `expand=` names a version of the same owner that comes *before* the
// half that waits for it, and one this release actually lists. It is bounded the way a
// source's `RulesFrom` floor is bounded — a self-declared number judged against the files the
// source can point at rather than trusted — because the owner's files apply in order and a
// number at or above the half's own version names an expansion no order of applying reaches
// first. SPECIFY's "its files apply in order" and `planOwner`'s "a contract half may not run in
// the same release as the expansion it removes" cannot both govern a file that names a later
// version, so the rule refuses it and says so, on a fresh installation as on an installed one:
// the fresh one is where an unbounded number does the damage, because the half applies, the
// ledger row is written, and the installation ends up holding a schema no other installation of
// the same release has. The ordinary pair — an expansion at a lower version, both files in one
// fresh release — applies, and stays applied here: every module's own test and every bootstrap
// migrates from nothing, and a guard that refused that would be a refusal to ship.
//
// Every assertion is on the catalogue and the ledger: which refusal came back, and which
// relations and history rows exist afterwards.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestAContractHalfWaitsForAVersionThisReleaseCanPointAt is the bound on `expand=`, in the
// two shapes the ledger cannot answer for itself. Both are a fresh installation, because
// that is the only installation where the release rule had nothing to say: an owner that has
// history reaches the ledger, which refuses a half whose partner has not applied.
func TestAContractHalfWaitsForAVersionThisReleaseCanPointAt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files fstest.MapFS
	}{
		{
			// The case the tenth review wrote, unpinned, and left for this round to
			// answer. The run applies a release's files in version order, so the half at
			// version 2 runs before the expansion at version 3 — the release that adds
			// the thing and takes it away in one go, which is the thing the guard is for.
			name: "the expansion the same release runs after the half",
			files: fstest.MapFS{
				"000001_orders.up.sql": {Data: []byte("CREATE TABLE orders (id bigint PRIMARY KEY, total text)")},
				"000002_drop.up.sql":   {Data: []byte("-- pkit: phase=contract\n-- pkit: expand=3\nALTER TABLE orders DROP COLUMN total")},
				"000003_amount.up.sql": {Data: []byte("ALTER TABLE orders ADD COLUMN amount_minor bigint")},
			},
		},
		{
			// The version is below the half, and no file carries it: the owner's versions
			// skip the one every installation is supposed to have applied. No order of
			// applying makes it true, and the ledger would record the wait as satisfied.
			name: "a version between the two files that no file is",
			files: fstest.MapFS{
				"000001_orders.up.sql": {Data: []byte("CREATE TABLE orders (id bigint PRIMARY KEY, total text)")},
				"000002_amount.up.sql": {Data: []byte("ALTER TABLE orders ADD COLUMN amount_minor bigint")},
				"000004_drop.up.sql":   {Data: []byte("-- pkit: phase=contract\n-- pkit: expand=3\nALTER TABLE orders DROP COLUMN total")},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "pointat", Files: tc.files})
			if err == nil || !strings.Contains(err.Error(), "refusal contract-without-expansion") {
				t.Fatalf("a contract half waited for a version this release does not have, and the run answered %v", err)
			}
			// The plan is read before a single statement of the owner runs, so the
			// database holds neither the layout the half would have narrowed nor the
			// ledger row that would have said it had.
			admin := dbtest.Open(t, migrateURL)
			for _, table := range []string{"orders"} {
				if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname = '"+table+"'"); n != 0 {
					t.Errorf("%d relations named %s: a release the plan refused applies nothing of its owner", n, table)
				}
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'pointat'"); n != 0 {
				t.Errorf("%d history rows for a release the plan refused", n)
			}
		})
	}

	// The shape the bound leaves alone, and has to: the ordinary expand/contract pair on a
	// fresh installation, where the expansion is a real earlier file of the same owner. A
	// guard that refused this one would refuse every module's own test and every bootstrap.
	migrateURL, _ := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "pair", Files: fstest.MapFS{
		"000001_orders.up.sql": {Data: []byte("CREATE TABLE orders (id bigint PRIMARY KEY, total text)")},
		"000002_amount.up.sql": {Data: []byte("ALTER TABLE orders ADD COLUMN amount_minor bigint")},
		"000003_drop.up.sql":   {Data: []byte("-- pkit: phase=contract\n-- pkit: expand=2\nALTER TABLE orders DROP COLUMN total")},
	}}); err != nil {
		t.Fatalf("a fresh installation runs the pair in order: %v", err)
	}
	if n := countRows(t, dbtest.Open(t, migrateURL), "SELECT count(*) FROM schema_migrations WHERE owner = 'pair'"); n != 3 {
		t.Errorf("%d files applied of the three, want all of them", n)
	}
}
