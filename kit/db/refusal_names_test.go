package db_test

// refusal_names_test.go is the ninth round's case for the eighth review's fourth finding.
// Two of the runner's runtime refusal ids — `data-table-missing` and
// `data-key-not-primary-key` — were written down only in the task's own specification:
// `grep -rn "data-table-missing" --include=*.go --include=*.md .` found the string in one
// reviewer's comment and nowhere else, so an operator handed the id by a log line or a
// runbook found nothing in the repository that refused them, and the id could drift from
// the sentence it stands for with no gate noticing.
//
// The fix is that the refusal prints its own id — `refusal <id>: <what was found>; <what
// to do>` — and this case reads it back off the message of a run that really refused. Each
// leg therefore asserts two things about the one sentence: the id in front of it, and the
// words that were the whole message before the id was added. Asserting only the id would
// pass a change that replaced the sentence with the id, which tells an operator less than
// they were told before; asserting only the sentence is what the repository did before this
// round, and it is what let the two names drift apart.
//
// The last leg reads this package's own README.md. The table there is the operator's copy of
// the same list, and a doc a test cannot disbelieve is a list rather than a gate. The
// paragraph under that table promises both halves — an id named there that nothing prints,
// and a printed id nothing names — and one case holding both meant writing the ids out a
// third time, which is what the ninth review measured: a literal list of four pinned the
// four names in both directions and the *set* in neither, so a fifth id added to either side
// passed. The ids are therefore read out of the places they come from rather than repeated:
// `refusalDoors` below is counted against what `refusals.go` declares, `refusals.go` is
// counted against README's table by `review8_refusal_ids_are_the_table_test.go`, and a
// refusal sentence written with an id inlined instead of declared is refused by name. Printed
// ⊆ declared ⊆ tabled ⊆ declared ⊆ printed: that chain is the paragraph, and a missing door
// case, table row or constant breaks it.

import (
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// refusalDoor is one runtime refusal and the run that reaches it.
type refusalDoor struct {
	name, id, says string
	sentinel       error
	// prior is what the installation already has applied. Only the release rule
	// needs one: a fresh owner installs its own layout, contract half included,
	// and the drain of an owner with no history is the migration's own work.
	prior fstest.MapFS
	files fstest.MapFS
}

// refusalDoors are the four runtime refusals and the doors that print them. The list is
// package-level because the case beside this one counts it: an id the runner declares, and
// README tables, and that no case here walks through the refusal that prints it, is exactly
// the drift the paragraph under README's table says this file refuses.
var refusalDoors = []refusalDoor{
	{
		name: "a table the window cannot resolve",
		id:   "data-table-missing",
		says: "does not exist here",
		files: fstest.MapFS{
			"000001_probe.up.sql":    {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, passes integer NOT NULL DEFAULT 0)")},
			"000002_backfill.up.sql": {Data: []byte("-- pkit: phase=data\n-- pkit: batch=10\n-- pkit: table=elsewhere\nUPDATE elsewhere SET passes = passes + 1 WHERE id IN (SELECT id FROM batch)")},
		},
	},
	{
		name: "a table keyed by two columns",
		id:   "data-key-not-primary-key",
		says: "no single-column primary key",
		files: fstest.MapFS{
			"000001_pairing.up.sql":  {Data: []byte("CREATE TABLE pairing (a bigint NOT NULL, b bigint NOT NULL, passes integer NOT NULL DEFAULT 0, PRIMARY KEY (a, b))")},
			"000002_backfill.up.sql": {Data: []byte("-- pkit: phase=data\n-- pkit: batch=10\n-- pkit: table=pairing\nUPDATE pairing SET passes = passes + 1 WHERE a IN (SELECT a FROM batch)")},
		},
	},
	{
		name: "a contract half beside its own expansion",
		id:   "contract-without-expansion",
		says: "waits for",
		prior: fstest.MapFS{
			"000001_orders.up.sql": {Data: []byte("CREATE TABLE orders (id bigint PRIMARY KEY, total text)")},
		},
		files: fstest.MapFS{
			"000001_orders.up.sql":     {Data: []byte("CREATE TABLE orders (id bigint PRIMARY KEY, total text)")},
			"000002_amount.up.sql":     {Data: []byte("ALTER TABLE orders ADD COLUMN amount_minor bigint")},
			"000003_drop_total.up.sql": {Data: []byte("-- pkit: phase=contract\n-- pkit: expand=2\nALTER TABLE orders DROP COLUMN total")},
		},
	},
	{
		name:     "a drain a migration could not finish inside its own bound",
		id:       "backfill-exceeds-install-budget",
		says:     "the bound an installation gives itself",
		sentinel: db.ErrBackfillBudget,
		files: fstest.MapFS{
			"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY, done boolean NOT NULL DEFAULT false);\nINSERT INTO probe (id) SELECT g FROM generate_series(1,60) g")},
			"000002_fill.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=1
-- pkit: table=probe
UPDATE probe SET done = true WHERE id IN (SELECT id FROM batch)`)},
		},
	},
}

func TestARuntimeRefusalNamesTheIDItsDocumentsGiveIt(t *testing.T) {
	for _, tc := range refusalDoors {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			if tc.prior != nil {
				if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "named", Files: tc.prior}); err != nil {
					t.Fatalf("the release this installation is one behind: %v", err)
				}
			}
			err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "named", Files: tc.files})
			if err == nil {
				t.Fatalf("the run that must refuse %s applied the files instead", tc.id)
			}
			if !strings.Contains(err.Error(), "refusal "+tc.id) {
				t.Errorf("the refusal %q does not print its id (%s): an operator grep of this repository finds the sentence, the id, or neither, and only the pair leads from a log line to the code that refused", err, tc.id)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the refusal %q no longer says %q: naming a refusal is not the same thing as explaining one", err, tc.says)
			}
			if tc.sentinel != nil && !errors.Is(err, tc.sentinel) {
				t.Errorf("the refusal %q is not the sentinel an operator's script matches with errors.Is", err)
			}
		})
	}
}

// declaredRefusalID is one refusal id and the constant the code calls it by, read off
// refusals.go — the file that owns the ids — rather than repeated here, because a copy of a
// list in the case that guards it is how the list stopped being guarded.
var declaredRefusalID = regexp.MustCompile(`(?m)^\s*(refusal[A-Za-z]*)\s*=\s*"([a-z0-9-]+)"\s*$`)

// inlinedRefusalID finds a refusal sentence whose id was written into the text instead of
// declared beside it. Such an id reaches an operator's log line from nowhere the table can
// check it against, which is the second half of the promise README's paragraph makes and the
// one no comparison of two lists can see.
var inlinedRefusalID = regexp.MustCompile(`"refusal ([a-z0-9-]+)`)

// TestEveryRefusalIDTheRunnerDeclaresIsOneARefusalPrints is the half of the promise the id
// comparison in review8_refusal_ids_are_the_table_test.go cannot make: that file holds
// refusals.go and README.md to be one list, and a list both of them name can still be a name
// nothing says. So every declared id has to be one of the doors above walked through a run
// that refused and read back off the message — and no id may be printed from a literal the
// declarations do not hold.
func TestEveryRefusalIDTheRunnerDeclaresIsOneARefusalPrints(t *testing.T) {
	source, err := os.ReadFile("refusals.go")
	if err != nil {
		t.Fatalf("kit/db/refusals.go, the file that names the ids: %v", err)
	}
	declared := map[string]bool{}
	for _, found := range declaredRefusalID.FindAllStringSubmatch(string(source), -1) {
		declared[found[2]] = true
	}
	if len(declared) == 0 {
		t.Fatal("refusals.go declares no refusal id at all, which is not what its comment says")
	}
	walked := map[string]bool{}
	for _, door := range refusalDoors {
		walked[door.id] = true
	}
	for id := range declared {
		if !walked[id] {
			t.Errorf("%q is declared by refusals.go and no case here walks a run that prints it: an id nothing prints is a name that drifts from the sentence it stands for with nothing noticing, which is the finding this file exists for", id)
		}
	}
	for _, door := range refusalDoors {
		if !declared[door.id] {
			t.Errorf("the door case %q prints %q, which refusals.go does not declare: the id an operator greps for has to have one home, and the sentence that prints it is not one", door.name, door.id)
		}
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("this package's own directory: %v", err)
	}
	var inlined []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		text, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		for _, found := range inlinedRefusalID.FindAllStringSubmatch(string(text), -1) {
			if !declared[found[1]] && !slices.Contains(inlined, found[1]) {
				inlined = append(inlined, found[1])
			}
		}
	}
	for _, id := range inlined {
		t.Errorf("a refusal sentence in this package prints %q, which refusals.go does not declare: the id is printed so that a log line leads to this repository, and a name with no constant behind it is in no list a gate can read", id)
	}
}

// TestTheRuntimeRefusalIDsAreTheOnesTheRunnerDocuments reads the operator's copy of the
// list out of this package's own README.md, beside the legs above that print each id. One
// without the other is the finding: a table that names an id nothing prints, or a sentence
// that prints one nothing names.
func TestTheRuntimeRefusalIDsAreTheOnesTheRunnerDocuments(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("this package's own README.md: %v", err)
	}
	for _, door := range refusalDoors {
		if !strings.Contains(string(readme), "`"+door.id+"`") {
			t.Errorf("README.md never names the refusal id %s, which a run prints: the table an operator reads and the sentence that refused them have to be one list", door.id)
		}
	}
}
