package db_test

// review7_bare_name_letters_test.go is the eighth review's case for the sentence ADR
// 0011 and migrations/README.md gained in 68c7560, and for the reader those sentences
// describe.
//
// ADR 0011: "each of those three reads the *name* the action carries in either spelling
// PostgreSQL takes it … a capture that stops at the bare identifier reads no action at
// all for exactly those columns, and the harm the rule is about ships". The commit that
// wrote that sentence closed one of the two ways a name escapes a capture that stops at
// the bare identifier — the quoted one — and left the other: the bare branch of
// `sqlName` is `[a-z_][\w.]*`, a bare identifier written with *ASCII* letters only.
// PostgreSQL's `ident_start` is `[A-Za-z_\200-\377]`: in a UTF-8 database a name carrying
// an accented letter is legal written bare, so `ALTER TABLE probe DROP atualizacao`
// (with a ç and an ã) is a name this reader cannot reach at all, and the three rules that
// find their operation in the name read no action for it.
//
// The round already accepted that domain one function away. `dollarTag` now takes "any
// byte ≥ 0x80" because "a tag is a name and a name takes the database's own letters, not
// ASCII alone" (migrations/README.md), and the reason it gives for not stopping at ASCII
// is the reason this case gives — the safe side of the guess is the side that reads too
// little. A tag it does not read leaves the value's words in the shape the window
// question is asked of; a name it does not read leaves the whole action out of the
// statement the rule reads, and the harm the rule names ships with the version in the
// ledger.
//
// Every leg asserts on the catalogue and the ledger — the type a column still has,
// whether a name is still there, how many history rows the run wrote — so no message can
// argue with it, and every family carries its ASCII control, which passes today and
// localises the fault to the letters in the name rather than to the question.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// accentedSchema: `b` and `n` are the controls; `atualização` is a name PostgreSQL takes
// written bare in a UTF-8 database, which is what every installation of this kernel runs
// under.
const accentedSchema = "CREATE TABLE probe (\n\tid bigint PRIMARY KEY,\n\tb text,\n\tn integer,\n\t" +
	"atualiza\u00e7\u00e3o text\n)"

func TestTheNameRulesReadTheBareNameWhateverItsLettersAre(t *testing.T) {
	for _, tc := range []struct {
		name, statement, rule, holds string
		want                         int
	}{
		{
			name:      "a type change on a name of ASCII letters, the control",
			statement: "ALTER TABLE probe ALTER COLUMN n TYPE bigint",
			rule:      "alter-column-type",
			holds:     "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'n' AND atttypid = 'integer'::regtype",
			want:      1,
		},
		{
			name:      "a type change on a name carrying the database's letters",
			statement: "ALTER TABLE probe ALTER COLUMN atualiza\u00e7\u00e3o TYPE varchar(20)",
			rule:      "alter-column-type",
			holds:     "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'atualiza\u00e7\u00e3o' AND atttypid = 'text'::regtype",
			want:      1,
		},
		{
			name:      "a dropped column named bare with ASCII letters, the control",
			statement: "ALTER TABLE probe DROP b",
			rule:      "drop-column",
			holds:     "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'b' AND NOT attisdropped",
			want:      1,
		},
		{
			name:      "a dropped column named bare with the database's letters",
			statement: "ALTER TABLE probe DROP atualiza\u00e7\u00e3o",
			rule:      "drop-column",
			holds:     "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'atualiza\u00e7\u00e3o' AND NOT attisdropped",
			want:      1,
		},
		{
			name:      "a NOT NULL column named bare with ASCII letters, the control",
			statement: "ALTER TABLE probe ADD c text NOT NULL",
			rule:      "add-column-not-null",
			holds:     "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'c' AND NOT attisdropped",
			want:      0,
		},
		{
			name:      "a NOT NULL column named bare with the database's letters",
			statement: "ALTER TABLE probe ADD n\u00e3ve text NOT NULL",
			rule:      "add-column-not-null",
			holds:     "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'n\u00e3ve' AND NOT attisdropped",
			want:      0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			files := fstest.MapFS{"000001_probe.up.sql": {Data: []byte(accentedSchema)}}
			if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "letters", Files: files}); err != nil {
				t.Fatalf("the owner's schema file: %v", err)
			}
			files["000002_alter.up.sql"] = &fstest.MapFile{Data: []byte(tc.statement)}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "letters", Files: files})
			admin := dbtest.Open(t, migrateURL)
			if err == nil {
				t.Errorf("%q applied with no refusal at all: the rule about it reads the name the action carries, and this name is bare to PostgreSQL and one letter set short of what the reader takes", tc.statement)
			} else if !strings.Contains(err.Error(), "rule "+tc.rule) {
				t.Errorf("the refusal %q does not name %s", err, tc.rule)
			}
			if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'letters' AND version = 2"); n != 0 {
				t.Errorf("the refused run wrote %d history rows: %q applied", n, tc.statement)
			}
			// The catalogue is the assertion that does not care which rule refused: a
			// file that shipped has already rewritten the table or taken the name away.
			if n := countRows(t, admin, tc.holds); n != tc.want {
				t.Errorf("the schema holds %d rows of the fact %q, want %d: the statement %q reached PostgreSQL, which is the harm rule %s names", n, tc.holds, tc.want, tc.statement, tc.rule)
			}
		})
	}
}
