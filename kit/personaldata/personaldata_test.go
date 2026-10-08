package personaldata

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
)

// houseSQL is one module's migrations in the house's own spellings, so the reader is
// exercised against the shapes the reference modules actually use: a CHECK constraint
// that holds a parenthesis, a column added in a later file with IF NOT EXISTS, a
// schema-qualified and quoted table name, and a comment that names columns no one
// created.
var houseSQL = fstest.MapFS{
	"000001_things.up.sql": {Data: []byte(`
-- The thing table. It carries an owner, a holder, and a note about the holder.
CREATE TABLE things (
	id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id    uuid NOT NULL,
	created_at   timestamptz NOT NULL DEFAULT now(),
	deleted_at   timestamptz,

	owner_id     uuid NOT NULL,
	note         text NOT NULL DEFAULT '',
	title        varchar(200) NOT NULL,
	payload      jsonb,

	CONSTRAINT things_title CHECK (length(title) <= 200)
);
`)},
	"000002_holder.up.sql": {Data: []byte(`
-- A second person per row, and a basket that is a row and not a person.
ALTER TABLE things ADD COLUMN IF NOT EXISTS holder_id uuid;
ALTER TABLE things ADD COLUMN note_max integer NOT NULL DEFAULT 500;
CREATE TABLE "cart"."carts" (
	id          uuid PRIMARY KEY,
	tenant_id   uuid NOT NULL,
	basket_id   uuid NOT NULL,
	placed_note text NOT NULL DEFAULT ''
);
`)},
	"000003_prose.up.sql": {Data: []byte(`
-- This sentence mentions ghost_id and holder_id in prose, which is not a column.
ALTER TABLE things ADD COLUMN holder_note text NOT NULL DEFAULT '';
`)},
}

// complete is what houseSQL has to be declared as: two uuid columns on things, one on
// carts, and the two text columns an identifier could sit in.
var complete = Implements(
	Table{
		Name:   "things",
		Person: "owner_id",
		About:  []string{"holder_id"},
		Text:   []string{"note", "payload", "holder_note"},
	},
	Table{
		Name:      "carts",
		NotPerson: []string{"basket_id"},
		Text:      []string{"placed_note"},
	},
)

// quietSQL is a module whose only uuid columns are the two every table has.
var quietSQL = fstest.MapFS{"000001_quiet.up.sql": {Data: []byte(`
CREATE TABLE quiet (
	id        uuid PRIMARY KEY,
	tenant_id uuid NOT NULL
);
`)}}

func check(t *testing.T, d Declaration, files fs.FS) []string {
	t.Helper()
	out := d.Check("thing", files)
	for _, problem := range out {
		if !strings.HasPrefix(problem, "module thing: ") {
			t.Errorf("refusal %q does not name the module it refuses", problem)
		}
	}
	return out
}

func joined(bad []string) string { return strings.Join(bad, "\n") }

// TestAModuleThatDeclaresNeitherTablesNorAReasonIsRefused is the whole point of the
// manifest field: the failure mode it exists for is silence, and the module that never
// thought about this has to be refused before it boots rather than after a person asks.
func TestAModuleThatDeclaresNeitherTablesNorAReasonIsRefused(t *testing.T) {
	bad := check(t, Declaration{}, quietSQL)
	if len(bad) != 1 || !strings.Contains(bad[0], "no reason for holding none") {
		t.Fatalf("an empty declaration: %v", bad)
	}
	if bad := check(t, complete, houseSQL); len(bad) != 0 {
		t.Fatalf("a complete declaration over the same SQL: %v", bad)
	}
}

func TestANoneDeclarationNeedsAReason(t *testing.T) {
	bad := check(t, Declaration{None: "   \t "}, quietSQL)
	if len(bad) != 1 || !strings.Contains(bad[0], "no reason") {
		t.Fatalf("a declaration of none with a blank reason: %v", bad)
	}
	if bad := check(t, None("a row and a tenant are the only uuids here"), quietSQL); len(bad) != 0 {
		t.Fatalf("the same module with the reason written down: %v", bad)
	}
	if bad := None("no table names a natural person").Check("admin", nil); len(bad) != 0 {
		t.Fatalf("a module with no SQL of its own holds no person column; refused as %v", bad)
	}
}

// TestANoneDeclarationRefusedByItsOwnSQL is the rule that keeps "none" honest: the
// reason is checked against the bytes, so a module cannot declare none and hold a
// person column anyway. This is the shape billing and site are actually in.
func TestANoneDeclarationRefusedByItsOwnSQL(t *testing.T) {
	sql := fstest.MapFS{"000001_billing.up.sql": {Data: []byte(`
CREATE TABLE billing_subscriptions (
	id        uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	plan_id   uuid NOT NULL
);
`)}}
	bad := check(t, None("no table names a natural person"), sql)
	if len(bad) != 1 {
		t.Fatalf("declares none over plan_id: %v", bad)
	}
	for _, want := range []string{"000001_billing.up.sql", "billing_subscriptions.plan_id", "no table names a natural person"} {
		if !strings.Contains(bad[0], want) {
			t.Errorf("refusal %q does not name %q; it has to say which file, which column and which claim contradicted", bad[0], want)
		}
	}
	// The same module declares the column instead and is believed.
	if bad := check(t, Implements(Table{Name: "billing_subscriptions", NotPerson: []string{"plan_id"}}), sql); len(bad) != 0 {
		t.Fatalf("plan_id declared as a row and not a person: %v", bad)
	}
}

// TestADeclarationCannotNameATableItsMigrationsDoNotCreate is house rule 3 as a boot
// refusal: naming another module's table here would be reading its rows.
func TestADeclarationCannotNameATableItsMigrationsDoNotCreate(t *testing.T) {
	bad := check(t, Implements(Table{Name: "users", Person: "id"}), quietSQL)
	if len(bad) != 1 || !strings.Contains(bad[0], `declares table "users", which none of its own migrations creates`) {
		t.Fatalf("a declaration over another module's table: %v", bad)
	}
}

func TestADeclarationCannotNameAColumnItsMigrationsDoNotCarry(t *testing.T) {
	sql := fstest.MapFS{"000001_t.up.sql": {Data: []byte(`
CREATE TABLE things (
	id        uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	owner_id  uuid NOT NULL
);
`)}}
	d := Implements(Table{Name: "things", Person: "owner_id", Text: []string{"email"}})
	bad := check(t, d, sql)
	if len(bad) != 1 || !strings.Contains(bad[0], "things.email") || !strings.Contains(bad[0], "do not carry") {
		t.Fatalf("a declaration over a column that is not there: %v", bad)
	}
}

// TestAColumnFiledAgainstTheWrongType asks the types the claims make: a person on a
// text column is a mistake about the schema, and a text claim on a uuid is one that
// would scrub nothing and report success.
func TestAColumnFiledAgainstTheWrongType(t *testing.T) {
	sql := fstest.MapFS{"000001_t.up.sql": {Data: []byte(`
CREATE TABLE things (
	id        uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	owner_id  uuid NOT NULL,
	title     varchar(200) NOT NULL,
	note      text NOT NULL DEFAULT ''
);
`)}}
	d := Implements(Table{
		Name:   "things",
		Person: "title",
		About:  []string{"note"},
		Text:   []string{"owner_id"},
	})
	bad := check(t, d, sql)
	if len(bad) != 3 {
		t.Fatalf("three claims against the wrong type, wanted three refusals: %v", bad)
	}
	for _, want := range []string{"things.title", "things.note", "things.owner_id"} {
		if !strings.Contains(joined(bad), want) {
			t.Errorf("no refusal names %s: %v", want, bad)
		}
	}
}

// TestAUuidColumnNobodyFiledIsRefused is the completeness rule, and the reason the
// kernel carries no list of likely column names: a module that invents sent_to_id is
// refused rather than quietly uncovered.
func TestAUuidColumnNobodyFiledIsRefused(t *testing.T) {
	partial := Implements(Table{Name: "things", Person: "owner_id"})
	bad := check(t, partial, houseSQL)
	if len(bad) != 2 {
		t.Fatalf("holder_id is unfiled and carts is unnamed: %v", bad)
	}
	if !strings.Contains(joined(bad), "things.holder_id") || !strings.Contains(joined(bad), "carts") {
		t.Errorf("the two things wrong here are holder_id and the missing carts table: %v", bad)
	}

	missingRole := Implements(
		Table{Name: "things", Person: "owner_id", Text: []string{"note", "payload", "holder_note"}},
		Table{Name: "carts", NotPerson: []string{"basket_id"}, Text: []string{"placed_note"}},
	)
	bad = check(t, missingRole, houseSQL)
	if len(bad) != 1 || !strings.Contains(bad[0], "holder_id") ||
		!strings.Contains(bad[0], "none of person, about, text or not-person") {
		t.Fatalf("a uuid column the declaration files as nothing: %v", bad)
	}
}

// TestAColumnFiledTwiceIsRefused: a column that is both the person and a row reference
// is a column nobody can write an erasure for.
func TestAColumnFiledTwiceIsRefused(t *testing.T) {
	d := Implements(Table{
		Name:      "things",
		Person:    "owner_id",
		About:     []string{"owner_id"},
		Text:      []string{"note", "payload", "holder_note"},
		NotPerson: []string{"holder_id"},
	}, Table{Name: "carts", NotPerson: []string{"basket_id"}, Text: []string{"placed_note"}})
	bad := check(t, d, houseSQL)
	if len(bad) != 1 || !strings.Contains(bad[0], "things.owner_id as both the person and a person it is about") {
		t.Fatalf("a column filed in two roles at once: %v", bad)
	}
}

// TestBothHalvesAtOnceIsAContradiction: a module that gives a reason for holding
// nothing and lists tables that hold something is describing two modules.
func TestBothHalvesAtOnceIsAContradiction(t *testing.T) {
	d := Declaration{Tables: complete.Tables, None: "no table names a natural person"}
	bad := check(t, d, houseSQL)
	if len(bad) != 1 || !strings.Contains(bad[0], "declares both") {
		t.Fatalf("both halves at once: %v", bad)
	}
}

func TestATableDeclaredTwiceIsRefused(t *testing.T) {
	d := Implements(
		Table{Name: "things", Person: "owner_id", About: []string{"holder_id"}, Text: []string{"note", "payload", "holder_note"}},
		Table{Name: "carts", NotPerson: []string{"basket_id"}, Text: []string{"placed_note"}},
		Table{Name: "things", Person: "owner_id"},
	)
	bad := check(t, d, houseSQL)
	if len(bad) != 1 || !strings.Contains(bad[0], `declares table "things" twice`) {
		t.Fatalf("one table declared twice: %v", bad)
	}
}

// TestAStatementThisReaderCannotPlaceIsRefusedRoughly names the shapes outside it — a
// table whose columns are all on one line, a column whose ALTER never arrives — and
// pins which way each fails. Each is a refusal: a reader that could not read the file
// says so, and Check turns that into a module that cannot boot.
func TestAStatementThisReaderCannotPlaceIsRefusedRoughly(t *testing.T) {
	oneLine := fstest.MapFS{"000001_t.up.sql": {Data: []byte(`
CREATE TABLE t (id uuid, tenant_id uuid, person_id uuid);
`)}}
	bad := check(t, None("a row and a tenant are the only uuids here"), oneLine)
	if len(bad) != 1 || !strings.Contains(bad[0], "000001_t.up.sql") ||
		!strings.Contains(bad[0], "CREATE TABLE") {
		t.Fatalf("a one-line CREATE TABLE the reader cannot read must name itself: %v", bad)
	}

	// A comment that names a column is not a column, and a schema-qualified table
	// arrives under the name its owner will declare it by. The house's migration files
	// are mostly comment: reading the prose would refuse tables that hold nothing.
	schema, err := ReadMigrations(houseSQL)
	if err != nil {
		t.Fatalf("reading the house's own spellings: %v", err)
	}
	if _, held := schema["things"]["ghost_id"]; held {
		t.Errorf("ghost_id exists only in a comment, and the schema holds it: %v", schema["things"])
	}
	if _, held := schema["things"]["holder_id"]; !held {
		t.Errorf("holder_id was added with IF NOT EXISTS and the schema does not hold it: %v", schema["things"])
	}
	if _, held := schema["cart.carts"]; held {
		t.Errorf(`CREATE TABLE "cart"."carts" is the table "carts", not "cart.carts"`)
	}
	if got, want := schema["carts"]["basket_id"].Type, "uuid not null"; got != want {
		t.Errorf("carts.basket_id type = %q, want %q", got, want)
	}
	if got, want := schema["things"]["title"].Type, "varchar(200) not null"; got != want {
		t.Errorf("things.title type = %q, want %q", got, want)
	}

	// A column added inside a DO block or a function body is read as a column,
	// because the text is a column the server may well add at some point. The
	// mistake runs the safe way: a module refused for a column it has to file.
	for name, sql := range map[string]string{
		"do block": `
DO $$ BEGIN
	ALTER TABLE t ADD COLUMN person_id uuid;
END $$;`,
		"dollar body": `
ALTER TABLE t ADD COLUMN filler text NOT NULL DEFAULT '';
CREATE FUNCTION f() RETURNS void AS $body$
	ALTER TABLE t ADD COLUMN person_id uuid;
$body$ LANGUAGE plpgsql;`,
	} {
		bad = check(t, None("no table names a natural person"),
			fstest.MapFS{"000001_t.up.sql": {Data: []byte(sql)}})
		if len(bad) == 0 {
			t.Errorf("%s: the reader answered \"no uuid columns\" about SQL that adds person_id: %q", name, sql)
		}
	}
}

func TestSubjectCarriesTheStringsTheLawAsksGone(t *testing.T) {
	s := Subject{
		ID:          uuid.MustParse("7f3c1a90-0000-4000-8000-000000000000"),
		Email:       "Jane@Example.com",
		DisplayName: "Jane Doe",
		Handle:      "  ",
	}
	if got := s.Token(); got != "<erased 7f3c1a90>" {
		t.Errorf("Token() = %q", got)
	}
	if got := s.ErasedEmail(); got != "erased-7f3c1a90-0000-4000-8000-000000000000@erased.invalid" {
		t.Errorf("ErasedEmail() = %q", got)
	}
	// Longest first, and the blank handle absent: replacing "" would rewrite every
	// column it touched, and a name inside the address goes with the address.
	if got, want := s.Identifiers(), []string{"Jane@Example.com", "Jane Doe"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Identifiers() = %q, want %q", got, want)
	}
	if got := (Subject{}).Identifiers(); len(got) != 0 {
		t.Errorf("a subject with no identifiers matched %q", got)
	}
}

func TestReadMigrationsReadsOnlyWhatMigrationsAre(t *testing.T) {
	schema, err := ReadMigrations(fstest.MapFS{
		"000001_x.up.sql":   {Data: []byte("CREATE TABLE t (\n\tid uuid\n);\n")},
		"000001_x.down.sql": {Data: []byte("DROP TABLE t;\n")},
		"README.md":         {Data: []byte("CREATE TABLE never (\n\tid uuid\n);\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := schema.Tables(); !reflect.DeepEqual(got, []string{"t"}) {
		t.Errorf("tables = %q, want only the one a .up.sql creates", got)
	}
	if got, want := schema["t"]["id"].Type, "uuid"; got != want {
		t.Errorf("id type = %q, want %q", got, want)
	}
}

// unreadableFS lists a file it cannot then read: a migration directory this reader is
// handed and cannot walk is a refusal, never a clean answer.
type unreadableFS struct{ fstest.MapFS }

func (u unreadableFS) ReadDir(string) ([]fs.DirEntry, error) {
	return nil, errors.New("no such directory")
}

func TestAMigrationDirectoryThatCannotBeWalkedIsARefusal(t *testing.T) {
	bad := None("no table names a natural person").Check("quiet", unreadableFS{MapFS: quietSQL})
	if len(bad) != 1 || !strings.Contains(bad[0], "cannot be read") {
		t.Fatalf("an unreadable migration directory: %v", bad)
	}
	if _, err := ReadMigrations(nil); err != nil {
		t.Errorf("a module with no SQL of its own is not an error to read: %v", err)
	}
}
