package module

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/personaldata"
)

// personSQL is a table that names a person: one uuid column beyond the row's own id
// and its tenant.
var personSQL = fstest.MapFS{"000001_note.up.sql": {Data: []byte(`
CREATE TABLE notes (
	id        uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	author_id uuid,
	body      text NOT NULL DEFAULT ''
);
`)}}

// quietSQL is a table with no uuid column beyond those two.
var quietSQL = fstest.MapFS{"000001_setting.up.sql": {Data: []byte(`
CREATE TABLE settings (
	id        uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	value     text NOT NULL
);
`)}}

func validateOne(t *testing.T, m Module) string {
	t.Helper()
	err := Validate([]Module{m})
	if err == nil {
		return ""
	}
	return err.Error()
}

// TestAModuleWithTablesThatSaysNothingAboutThemIsRefused is the reason the field
// exists. A module that adds `author_id` and never declares it is invisible to a
// person's export request and to their erasure, and today nothing in the boot says
// so; the refusal lands on the manifest and the migration bytes, before a database is
// opened, because the thing being refused is a claim about a file.
func TestAModuleWithTablesThatSaysNothingAboutThemIsRefused(t *testing.T) {
	m := Module{Name: "notes", Migrations: personSQL}
	got := validateOne(t, m)
	if !strings.Contains(got, "module \"notes\": declares no personal-data table") {
		t.Fatalf("a module with tables and no declaration: %q", got)
	}

	declared := m
	declared.PersonalData = personaldata.Implements(personaldata.Table{
		Name:   "notes",
		Person: "author_id",
		Text:   []string{"body"},
	})
	if got := validateOne(t, declared); got != "" {
		t.Fatalf("the same module with the claim written down: %q", got)
	}

	forgotten := declared
	forgotten.Migrations = fstest.MapFS{"000001_note.up.sql": {Data: []byte(`
CREATE TABLE notes (
	id         uuid PRIMARY KEY,
	tenant_id  uuid NOT NULL,
	author_id  uuid,
	body       text NOT NULL DEFAULT '',
	holder_id  uuid
);
`)}}
	if got := validateOne(t, forgotten); !strings.Contains(got, "notes.holder_id") {
		t.Fatalf("a uuid column the declaration files as nothing: %q", got)
	}
	// What the boot cannot see is which of its text columns carries an identifier:
	// a text column names nobody until a person writes their address into it, so the
	// module's own claim is the only source, and a column it leaves out of `Text` is
	// ADR 0020's erasure verification refusing later rather than a lie refused now.
	leftOut := declared
	leftOut.PersonalData = personaldata.Implements(personaldata.Table{
		Name:   "notes",
		Person: "author_id",
	})
	if got := validateOne(t, leftOut); got != "" {
		t.Fatalf("a text column nobody filed is not a boot refusal, and the erasure is what checks it: %q", got)
	}
}

// TestAModuleWithNoTablesOfItsOwnIsNotAsked: the claim is about columns, and a module
// that creates no table creates no person column. Asking the shell for a sentence
// would teach that the field is a formality a module fills in to be quiet.
func TestAModuleWithNoTablesOfItsOwnIsNotAsked(t *testing.T) {
	if got := validateOne(t, Module{Name: "shell"}); got != "" {
		t.Fatalf("a module with no migrations: %q", got)
	}
	if got := validateOne(t, Module{Name: "settings", Migrations: quietSQL}); !strings.Contains(got, "no reason for holding none") {
		t.Fatalf("a module with tables and no claim: %q", got)
	}
	quiet := Module{Name: "settings", Migrations: quietSQL,
		PersonalData: personaldata.None("a setting is a tenant's own choice: the only uuids are the row's id and its tenant")}
	if got := validateOne(t, quiet); got != "" {
		t.Fatalf("the same module with the reason written down: %q", got)
	}
}

// TestAReasonThatContradictsTheSQLIsRefused keeps `none` from becoming the box
// somebody ticks. The reason is a claim about this module's files, so the files
// answer it.
func TestAReasonThatContradictsTheSQLIsRefused(t *testing.T) {
	m := Module{
		Name:         "notes",
		Migrations:   personSQL,
		PersonalData: personaldata.None("no table of ours names a person"),
	}
	got := validateOne(t, m)
	if !strings.Contains(got, "000001_note.up.sql") || !strings.Contains(got, "notes.author_id") ||
		!strings.Contains(got, "no table of ours names a person") {
		t.Fatalf("a `none` the module's own SQL contradicts: %q", got)
	}
}
