package db_test

// review11_a_contract_half_waits_whether_or_not_the_owner_has_history_test.go is the tenth
// round's acceptance-review case for the release rule the brief names: "the runner refuses a
// `contract` migration whose `expand` partner has not been applied for at least one release
// (recorded in the ledger by version)".
//
// `kit/db/README.md` states the guard without an condition on the installation: "A file the
// rule table refuses, a contract half whose expansion has not applied, and a grammar mistake in
// a header are all reported before anything of that owner is applied." `migrations/README.md`
// says the contract half "refuses while its `expand=` version is not already in the
// installation's history", and calls it one of two rules "about a release rather than a file,
// and neither can be enforced halfway through one".
//
// `planOwner` answers it for an owner that has history, and returns the owner's files
// unanswered for one that has none (`if history.latest[owner] == 0 { return files, nil }`). For a
// contract half that names a version the owner never offers, the consequence is not a weaker
// guard on an empty database but a database whose schema differs from every other installation
// of the same release: the version that was supposed to make the contract safe has not run and
// never will, and the ledger records the contract as applied. The first leg is that file, and
// asserts the two things that cannot both be true of it — either the run refuses, or the column
// its expand half adds is really there.
//
// The second leg is the same half beside an expansion the same release offers at a later
// version. There the run applies the contract first and the expansion after it, which is what
// SPECIFY's scope ("for an owner that already has history", line 154) and kit/db/README's
// "An owner with no history at all is the exception both times: nobody is reading, and its files
// apply in order" both appear to accept; the case is recorded here and not asserted, because
// which way it is answered is the round's call. What is asserted is the first leg, where there
// is no order to apply in: the file names a version the owner has never had and never will.
//
// The third and fourth legs are the controls that hold today and have to hold after any fix:
// the installed database, which refuses by name and applies nothing, and the ordinary shape — an
// expansion, then a contract half a release later — which applies.
//
// Every assertion is on the catalogue and the ledger, never on a message: whether the refusal
// names the rule the runner declares, and which columns and history rows exist afterwards.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

const review11Orders = "CREATE TABLE orders (id bigint PRIMARY KEY, total text)"

func TestAContractHalfRefusesWhileTheVersionItWaitsForHasNotApplied(t *testing.T) {
	for _, tc := range []struct {
		name  string
		prior fstest.MapFS
		files fstest.MapFS
	}{
		{
			// The file that names nothing the owner ever offers. README's sentence and the
			// brief's rule both refuse it; planOwner's first line waves it through.
			name: "a first installation, and an expansion this owner never had a version for",
			files: fstest.MapFS{
				"000001_orders.up.sql": {Data: []byte(review11Orders)},
				"000002_drop.up.sql":   {Data: []byte("-- pkit: phase=contract\n-- pkit: expand=9\nALTER TABLE orders DROP COLUMN total")},
				"000003_items.up.sql":  {Data: []byte("CREATE TABLE line_items (id bigint PRIMARY KEY, order_id bigint)")},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "release", Files: tc.files})
			admin := dbtest.Open(t, migrateURL)
			if err == nil {
				// Not a message to read: the state the guard exists to make impossible. The
				// ledger says the contract ran, and the version it waited for has not.
				if n := countRows(t, admin, "SELECT count(*) FROM information_schema.columns WHERE table_name = 'orders' AND column_name = 'amount_minor'"); n == 0 {
					t.Errorf("the contract half applied while its expand half has not: the ledger carries %s/%s and the column it waits for is not in this database, which is the state the rule exists to refuse", "release", "000002")
				}
				t.Fatalf("a contract half whose expansion has not been applied for at least one release was applied: %v", err)
			}
			if !strings.Contains(err.Error(), "refusal contract-without-expansion") {
				t.Fatalf("the run refused for something other than the release rule: %v", err)
			}
			// The rule reads the plan before any of the owner runs, so nothing of it is here.
			for _, table := range []string{"orders", "line_items"} {
				if n := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname = '"+table+"'"); n != 0 {
					t.Errorf("%d relations named %s: a refused release applies nothing of its owner", n, table)
				}
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'release'"); n != 0 {
				t.Errorf("%d history rows for a release the plan refused", n)
			}
		})
	}
}

// TestAContractHalfIsRefusedOnAnInstalledOwnerAndAppliedOnTheReleaseAfter is the pair of
// controls: the same first release, refused once its owner has history (which is the leg every
// round has run), and the ordinary shape — expansion applied, contract half shipped afterwards
// — which applies. A fix for the legs above has to keep both answers.
func TestAContractHalfIsRefusedOnAnInstalledOwnerAndAppliedOnTheReleaseAfter(t *testing.T) {
	t.Run("installed owner, expansion not applied", func(t *testing.T) {
		migrateURL, _ := dbtest.URLs(t)
		if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "late", Files: fstest.MapFS{
			"000001_orders.up.sql": {Data: []byte(review11Orders)},
		}}); err != nil {
			t.Fatalf("the release this installation is one behind: %v", err)
		}
		err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "late", Files: fstest.MapFS{
			"000001_orders.up.sql": {Data: []byte(review11Orders)},
			"000002_drop.up.sql":   {Data: []byte("-- pkit: phase=contract\n-- pkit: expand=3\nALTER TABLE orders DROP COLUMN total")},
			"000003_amount.up.sql": {Data: []byte("ALTER TABLE orders ADD COLUMN amount_minor bigint")},
		}})
		if err == nil || !strings.Contains(err.Error(), "refusal contract-without-expansion") {
			t.Fatalf("the installed owner's contract half was not refused by name: %v", err)
		}
		admin := dbtest.Open(t, migrateURL)
		if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'late'"); n != 1 {
			t.Errorf("%d history rows, want the one file of the first release: a refused plan wrote past it", n)
		}
	})

	t.Run("expansion applied a release ago", func(t *testing.T) {
		migrateURL, _ := dbtest.URLs(t)
		if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "next", Files: fstest.MapFS{
			"000001_orders.up.sql": {Data: []byte(review11Orders)},
			"000002_amount.up.sql": {Data: []byte("ALTER TABLE orders ADD COLUMN amount_minor bigint")},
		}}); err != nil {
			t.Fatalf("the expansion, applied in its own release: %v", err)
		}
		if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "next", Files: fstest.MapFS{
			"000001_orders.up.sql": {Data: []byte(review11Orders)},
			"000002_amount.up.sql": {Data: []byte("ALTER TABLE orders ADD COLUMN amount_minor bigint")},
			"000003_drop.up.sql":   {Data: []byte("-- pkit: phase=contract\n-- pkit: expand=2\nALTER TABLE orders DROP COLUMN total")},
		}}); err != nil {
			t.Fatalf("the contract half, one release after the expansion it removes: %v", err)
		}
		admin := dbtest.Open(t, migrateURL)
		if n := countRows(t, admin, "SELECT count(*) FROM information_schema.columns WHERE table_name = 'orders' AND column_name = 'total'"); n != 0 {
			t.Errorf("%d columns named total: the contract half did not run", n)
		}
	})
}
