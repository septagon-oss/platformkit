package db_test

// review13_a_catalog_count_by_name_alone_sees_another_tests_schema_test.go is the
// thirteenth round's case for the hermeticity the twelfth round's ruling asked for.
//
// That ruling rewrote one leg of
// `review11_a_word_inside_a_value_is_not_a_statement_test.go` because it counted a table no
// run the case permits could create, and the rewrite reads the catalogue with
// `relnamespace = current_schema()::regnamespace` — the shape that survives another schema
// holding a relation of the same name. Its comment then says "its sibling legs at :115 and
// :140 ask `pg_class` for the same reason". They do not: those legs, and seventeen others in
// this package, filter the catalogue by a *bare name*, which PostgreSQL matches across every
// schema in the cluster, while `dbtest.URLs` gives each case a schema of its own
// (`kit/db/dbtest/dbtest.go:44-78`).
//
// It is not theoretical. `go test -timeout` kills the binary, and a killed case never runs
// its `DROP SCHEMA … CASCADE`; the schema it was using stays, with its `probe` inside, and
// every one of these legs then reports a relation the case never created. The twelfth round
// met exactly this and wrote down the cure it had used ("Dropping
// `t_38fibyx6w3pci_testadrainresumesfromarowthatholdsnokeyyet` made all six pass"). The leg
// the review rewrote, and `review12_a_marker_the_reader_cannot_read_is_refused_and_never_ignored_test.go:95`,
// already count through `current_schema()`.
//
// Reproduction of the failure the missing predicate causes, on an otherwise clean tree:
//
//	psql … -qc "CREATE SCHEMA review13leftover; CREATE TABLE review13leftover.probe(id bigint)"
//	go test ./kit/db -count=1 -run 'TestAnAutocommitFileWhoseOnlyConcurrentlyIsDataIsStillRefused|…'
//	→ 12 legs FAIL with "1 relations named probe", in cases whose own schema holds nothing
//
// A name resolved through `search_path` (`'probe'::regclass`, `to_regclass('probe')`) is
// hermetic by construction and is not what this refuses, and neither is a query that names
// its schema. The needles below are built rather than written so that this file, which lives
// beside the files it reads, does not report itself: no line here spells the pair it looks
// for.

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// review13Catalog maps a catalogue view to the columns a bare-name filter on it matches
// anywhere in the cluster, and review13FilterKey to the predicates that make the count
// this schema's own.
var (
	review13Catalog   = map[string][]string{"pg_" + "class": {"rel" + "name"}, "pg_" + "indexes": {"index" + "name", "table" + "name"}}
	review13FilterKey = []string{"rel" + "namespace", "schema" + "name", "::" + "regclass"}
)

func TestACatalogCountOfOneTestsOwnSchemaNamesItsSchema(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), "_test.go") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	var found []string
	for _, name := range files {
		text, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, site := range review13UnqualifiedCounts(string(text)) {
			found = append(found, name+":"+site)
		}
	}
	if len(found) > 0 {
		t.Errorf("%d legs count %s by a bare name, which PostgreSQL matches in every schema, while dbtest gives this package's cases one schema each; the count has to name the schema (`%s = current_schema()::regnamespace`, `%s = current_schema()`) or resolve the name through search_path:\n\t%s",
			len(found), strings.Join(review13Keys(), " and "),
			review13FilterKey[0], review13FilterKey[1], strings.Join(found, "\n\t"))
	}
}

// review13UnqualifiedCounts returns one line per query in this source that filters one of
// the catalogue views by name and never says which schema it means. A query is read as the
// Go string literal it is written as — including the `"…" + "…"` a long query is wrapped in,
// which is where `review_guarantees_test.go` keeps its own filter — so the answer is about
// the SQL the case runs, not about where its words happen to sit.
func review13UnqualifiedCounts(text string) []string {
	var found []string
	for _, needle := range review13Needles() {
		for at := 0; at < len(text); {
			i := strings.Index(text[at:], needle)
			if i < 0 {
				break
			}
			i += at
			query, line := review13Literal(text, i)
			if !review13Filtered(query) {
				found = append(found, strconv.Itoa(line)+": "+strings.Join(strings.Fields(query), " "))
			}
			at = i + len(needle)
		}
	}
	sort.Strings(found)
	return found
}

func review13Needles() []string {
	var needles []string
	for table, columns := range review13Catalog {
		for _, column := range columns {
			needles = append(needles, table+" WHERE "+column)
		}
	}
	sort.Strings(needles)
	return needles
}

// review13Literal returns the whole string literal containing index i — every `"…"` segment
// the `+` operator joins to it — and the line the literal starts on.
func review13Literal(text string, i int) (query string, line int) {
	line = strings.Count(text[:i], "\n") + 1
	start := strings.LastIndexByte(text[:i], '"')
	for end := start; end >= 0 && end < len(text); {
		closing := strings.IndexByte(text[end+1:], '"')
		if closing < 0 {
			break
		}
		end = end + 1 + closing
		query += text[start+1 : end]
		rest := strings.TrimLeft(text[end+1:], " \t\n")
		if !strings.HasPrefix(rest, "+") {
			break
		}
		next := strings.IndexByte(rest, '"')
		if next < 0 {
			break
		}
		end = len(text) - len(rest) + next
		start = end
	}
	return query, line
}

func review13Filtered(query string) bool {
	for _, key := range review13FilterKey {
		if strings.Contains(query, key) {
			return true
		}
	}
	return false
}

func review13Keys() []string {
	var out []string
	for k := range review13Catalog {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
