package db_test

// review6_contract_partner_test.go is the seventh review's case for the sentence on
// `partnerFile` in kit/db/migrate.go: it "names the version a contract half waits for
// as a file, because that is what an operator greps for and what a release note
// lists".
//
// A `phase=contract` file has to carry `expand=<version>`, and the grammar checks that
// the number is a version — nothing checks that the number is a version this owner
// ever shipped. The refusal that reaches the operator for `expand=99` on an owner whose
// files are 1 and 2 calls `partnerFile`, which returns the empty string when no file
// carries the version, so the sentence reads "waits for  of the same owner" and names
// neither the version nor the fact that no such version exists. The installation is
// stuck behind a wait nothing can ever satisfy, and the message an operator greps is
// blank.
//
// The control leg is the case the comment describes — a partner the owner did ship,
// pending in this run — and it passes today, which is what localises the fault to the
// branch where the name is missing rather than the refusal that is not there.

import (
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestAContractHalfNamesTheVersionItWaitsFor(t *testing.T) {
	for _, tc := range []struct {
		name   string
		expand string
		want   string
	}{
		{
			name:   "the partner this owner shipped, pending in this same run",
			expand: "2",
			want:   "000002_widen.up.sql",
		},
		{
			name:   "a version this owner never shipped",
			expand: "99",
			want:   "99",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{
				"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, a text)")},
			}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "partner", Files: files}); err != nil {
				t.Fatalf("the owner's first file: %v", err)
			}
			files["000002_widen.up.sql"] = &fstest.MapFile{Data: []byte("ALTER TABLE probe ADD b text")}
			files["000003_narrow.up.sql"] = &fstest.MapFile{Data: []byte("-- pkit: phase=contract\n-- pkit: expand=" + tc.expand + "\nALTER TABLE probe DROP b")}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "partner", Files: files})
			if err == nil {
				t.Error("a contract half applied in the run that carried its expansion")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal %q names neither %s nor where to look for it", err, tc.want)
			}
			admin := dbtest.Open(t, migrateURL)
			for _, version := range []int64{2, 3} {
				if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'partner' AND version = "+strconv.FormatInt(version, 10)); n != 0 {
					t.Errorf("the refused run wrote history for version %d (%d rows)", version, n)
				}
			}
			if n := countRows(t, admin, "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'a' AND NOT attisdropped"); n != 1 {
				t.Errorf("the column the run left alone is not there (%d present)", n)
			}
		})
	}
}
