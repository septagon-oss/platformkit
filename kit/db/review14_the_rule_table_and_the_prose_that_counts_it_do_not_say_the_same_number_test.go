package db_test

// review14_the_rule_table_and_the_prose_that_counts_it_do_not_say_the_same_number_test.go
// asks the two documents that publish the rule table whether they hold the same number as
// the table itself.
//
// `migrations/README.md` writes, in the paragraph under its own rule table:
//
//	"Seven refusals have no `allow=` to answer them — the executor's three (…), and the four
//	rules whose exception column says `none`"
//
// and `docs/adr/0011-migration-ownership.md` repeats the count ("those four rules and the
// executor's three"). The table the sentence sits under has eleven rules, five of which
// carry an exception cell that begins `none`: `index-concurrent-without-autocommit`,
// `autocommit-without-concurrently`, `autocommit-not-rerunnable`, `data-with-ddl` and
// `unused-allow`. The last of those is a refusal no `allow=` answers — its own exception
// cell says so, and the code that raises it exists precisely because the marker in the file
// excepts nothing — so a reader who counts the refusals that have no `allow=` from the table
// gets eight, and a reader who trusts the prose gets seven.
//
// The four is right about the *code*: `kit/db/migration_rules.go` holds ten entries, six of
// them with an `exception:` field, four without, and `unused-allow` lives outside that
// table. That is exactly why the sentence needs the table's number rather than the code's:
// it points at the table ("whose exception column says `none`"), and the count is the
// operator-facing fact — it decides which refusal an author reading the guide believes they
// can contest. A count that has to be kept in sync by reading is the same fault
// `review8_refusal_ids_are_the_table_test.go` refuses for the four ids below the other table,
// and this case is the same shape: read the table, read the sentence, refuse the two to
// differ.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var spelledNumbers = map[string]int{
	"zero": 0, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
	"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12,
}

func TestTheRuleTableAndTheProseThatCountsItSayTheSameNumber(t *testing.T) {
	doc := readDoc(t, "../../migrations/README.md")
	noneRules := rulesWithNoException(t, doc)

	// The sentence under the table, in the document that holds the table.
	guide := regexp.MustCompile(`(?i)the (one|two|three|four|five|six|seven|eight|nine|ten) rules whose exception column says`)
	m := guide.FindStringSubmatch(doc)
	if m == nil {
		t.Fatal(`migrations/README.md no longer says "the <n> rules whose exception column says ` + "`none`" + `"; this case reads that sentence, so point it at whatever sentence now carries the count rather than dropping the question`)
	}
	claim := spelledNumbers[strings.ToLower(m[1])]
	if claim != len(noneRules) {
		t.Errorf("migrations/README.md says %d rules carry an exception of `none`; the table it publishes holds %d (%s) — the prose and the table are two lists of the same set and they have come apart",
			claim, len(noneRules), strings.Join(noneRules, ", "))
	}

	// The same count, in the ADR that the brief asks to hold the rule.
	adr := readDoc(t, "../../docs/adr/0011-migration-ownership.md")
	adrRe := regexp.MustCompile(`(?i)those (one|two|three|four|five|six|seven|eight|nine|ten) rules`)
	if a := adrRe.FindStringSubmatch(adr); a != nil {
		if n := spelledNumbers[strings.ToLower(a[1])]; n != len(noneRules) {
			t.Errorf("docs/adr/0011 says %d rules have no exception; the published table holds %d (%s)",
				n, len(noneRules), strings.Join(noneRules, ", "))
		}
	}

	// The total the guide leads with, which is the number an operator answers to: the
	// executor's three plus whatever the table says carries no exception.
	total := regexp.MustCompile(`(?i)\b(one|two|three|four|five|six|seven|eight|nine|ten) refusals have no`)
	if g := total.FindStringSubmatch(doc); g != nil {
		if want := 3 + len(noneRules); spelledNumbers[strings.ToLower(g[1])] != want {
			t.Errorf("migrations/README.md opens with %s refusals having no `allow=` to answer them; the executor's three plus the table's %d rules without an exception is %d",
				strings.ToLower(g[1]), len(noneRules), want)
		}
	}
}

// rulesWithNoException reads the rule table out of the guide — the rows under the header
// `| rule | fires on | why | exception |`, nothing else — and returns the names of the rows
// whose exception cell begins with `none`.
func rulesWithNoException(t *testing.T, doc string) []string {
	t.Helper()
	var names []string
	inTable := false
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "| rule |") {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		if !strings.HasPrefix(trimmed, "|") {
			break
		}
		cells := strings.Split(trimmed, "|")
		if len(cells) < 5 {
			continue
		}
		rule := strings.Trim(strings.TrimSpace(cells[1]), "`")
		exception := strings.TrimSpace(cells[4])
		if rule == "" || rule == "rule" || strings.HasPrefix(strings.TrimSpace(cells[2]), "---") {
			continue
		}
		if strings.HasPrefix(exception, "none") {
			names = append(names, rule)
		}
	}
	if len(names) == 0 {
		t.Fatal("no rule row in migrations/README.md carries an exception beginning `none`: the table this case reads has changed shape, and the count it publishes is no longer checkable this way")
	}
	return names
}

func readDoc(t *testing.T, path string) string {
	t.Helper()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(text)
}
