package db_test

// review15_every_unexceptable_refusal_is_named_in_the_division_below_the_table_test.go
//
// Two sentences in `migrations/README.md` promise an enumeration, and the enumeration has one
// fewer member than the promise:
//
//	"Eight refusals have no `allow=` to answer them — the executor's three (…), and the five
//	rules whose exception column says `none`"
//	"… The paragraph below the table names which reading each of the eight is asked of, and what
//	 that gives up."
//
// `docs/adr/0011-migration-ownership.md` repeats the same promise for the same set ("Which text
// each of the eight refusals with no `allow=` is asked of — those five rules and the executor's
// three"). What the paragraphs below the table then divide is three + two + two:
//
//   - "Three — the executor's `a data file is one statement`, `data-with-ddl` and
//     `autocommit-not-rerunnable` — are decided from the cut PostgreSQL makes";
//   - "The executor's other two are the window's own shape";
//   - "The other two ask after one word, `CONCURRENTLY`".
//
// Seven. The fifth rule with no exception, `unused-allow`, is named twice on the page — both
// times *above* the table, in the paragraph that says an author who excepts a rule that never
// fired is refused for writing it. Nothing below the table says which text it is asked of, and
// it is the one refusal of the eight whose answer is not a question about the file's text at all:
// it fires when another rule did not. That is a fourth kind of question, and the sentence that
// promises all eight are divided by the question each asks is true of seven.
//
// The count is the fact an operator acts on, and this task has already learned that twice: the
// guide's total used to say seven where its own table said eight, and
// `review14_the_rule_table_and_the_prose_that_counts_it_do_not_say_the_same_number_test.go`
// holds that boundary by reading the table rather than trusting the prose. This case is the same
// shape one step on: it reads the set the table publishes, then asks the document to say what it
// reads each of them of. Either fix passes — a sentence below the table for the eighth, or a
// promise that names the seven it divides — because what the case refuses is a count and a list
// that disagree with the document holding them.

import (
	"strings"
	"testing"
)

func TestTheDivisionBelowTheRuleTableNamesWhichReadingEachUnexceptableRefusalIsAskedOf(t *testing.T) {
	doc := readDoc(t, "../../migrations/README.md")
	noneRules := rulesWithNoException(t, doc)

	// The promise, in the words the guide uses. If the sentence goes away this case has to be
	// pointed at whatever carries the promise, which is what its failure says.
	promise := strings.ToLower(doc)
	if !strings.Contains(promise, "names which reading each of the eight") &&
		!strings.Contains(promise, "names which reading each of the seven") {
		t.Fatal(`migrations/README.md no longer promises that the paragraph below the table names "which reading each of the eight is asked of"; this case reads that promise, so point it at the sentence that carries it rather than dropping the question`)
	}

	below := belowRuleTable(t, doc)
	var unnamed []string
	for _, rule := range noneRules {
		if !strings.Contains(strings.ToLower(below), "`"+strings.ToLower(rule)+"`") {
			unnamed = append(unnamed, rule)
		}
	}
	if len(unnamed) > 0 {
		t.Errorf("migrations/README.md publishes %d rules whose exception column says `none` and divides the refusals below its table, but never says which reading %v %s asked of; the paragraph divides three + two + two, and a refusal nobody can except is exactly the one whose reading the guide owes an author",
			len(noneRules), unnamed, verb(len(unnamed)))
	}
}

// belowRuleTable is the guide after its rule table ends — the text the promise points at. The
// table is the block of rows under `| rule | … `, which is the block
// rulesWithNoException reads.
func belowRuleTable(t *testing.T, doc string) string {
	t.Helper()
	lines := strings.Split(doc, "\n")
	start, end := -1, -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "| rule |") {
			start = i
			end = i
			continue
		}
		if start < 0 {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			break
		}
		end = i
	}
	if start < 0 || end == start {
		t.Fatal("migrations/README.md has no rule table to read a division from")
	}
	return strings.Join(lines[end+1:], "\n")
}

func verb(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}
