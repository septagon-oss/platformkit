package personaldata

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// houseBounds is the CHECK text the server prints for the constraints this repository
// actually carries, taken from pg_get_constraintdef on a database the migrations were
// applied to, together with the shapes it has to refuse: a bound that sits behind an OR,
// a length the constraint measures for a column nobody scrubs, an equality, a pattern, and
// a literal that holds both a parenthesis and the word AND.
var houseBounds = []struct {
	// why names the constraint, or the shape this case exists to keep out of the answer.
	why string

	// clause is what pg_get_constraintdef answers, or what a CHECK of that shape looks like.
	clause string

	// asked is the set of columns the scrub named, which is the set the reader may answer
	// for: a bound over anything else is another column's business.
	asked []string

	// want is the bound each asked-for column gains, and an absent column gained nothing.
	want map[string]extent
}{
	{
		// contents (000042): the body is a text column bounded by rule rather than by
		// type, in both measures at once. The erasure has to fit inside both.
		why:    "contents_body, both measures",
		asked:  []string{"body"},
		clause: "CHECK (((char_length(body) <= 262144) AND (octet_length(body) <= 1048576)))",
		want:   map[string]extent{"body": {chars: 262144, octets: 1048576}},
	},
	{
		// direct_mail_deliveries (000044): the bound stands beside a clause that measures
		// nothing, which is an AND and so still guarantees the bound.
		why:    "a bound beside a clause that measures nothing",
		asked:  []string{"reason"},
		clause: "CHECK (((length(reason) <= 500) AND ((outcome = 'sent'::text) OR (reason <> ''::text))))",
		want:   map[string]extent{"reason": {chars: 500}},
	},
	{
		// passkey_credentials (000035): a size written as a range reaches the reader as the
		// two comparisons the parser made of it, of which the ceiling is the useful half.
		why:    "a range, which is stated as BETWEEN rather than as <=",
		asked:  []string{"credential_id"},
		clause: "CHECK (((octet_length(credential_id) >= 1) AND (octet_length(credential_id) <= 1024)))",
		want:   map[string]extent{"credential_id": {octets: 1024}},
	},
	{
		// platformkit_outbox (000052): the whole bound sits inside an OR, so a row may
		// hold more than it mentions. Reading 512 here would cut a long value down to it.
		why:    "a bound behind an OR is not a bound",
		asked:  []string{"source_file"},
		clause: "CHECK (((source_file IS NULL) OR ((source_file <> ''::text) AND (length(source_file) <= 512))))",
		want:   map[string]extent{},
	},
	{
		// The same rule written the way a product would write it: the ceiling is one of
		// two states, and a scrub that read 10 here would destroy the long form.
		why:    "a bound one alternative of an OR",
		asked:  []string{"note"},
		clause: "CHECK (((length(note) <= 10) OR (kind = 'long'::text)))",
		want:   map[string]extent{},
	},
	{
		// A varchar column: the deparser writes the coercion the parser inserted, and the
		// bound is still the column's own.
		why:    "a length over a coerced varchar",
		asked:  []string{"title"},
		clause: "CHECK ((char_length((title)::text) <= 200))",
		want:   map[string]extent{"title": {chars: 200}},
	},
	{
		// translations (000043): a length the constraint fixes rather than caps. The value
		// cannot grow at all, but the number is not a ceiling an erasure may fill.
		why:    "an equality is not a ceiling",
		asked:  []string{"source_hash"},
		clause: "CHECK ((char_length(source_hash) = 64))",
		want:   map[string]extent{},
	},
	{
		// passkey_credentials (000035) again: three clauses, one of them a bound, one a
		// function call that holds a parenthesis, and a literal in the last.
		why:    "a shape, a pattern and a bound in one constraint",
		asked:  []string{"name", "kind"},
		clause: "CHECK (((name = btrim(name)) AND (char_length(name) <= 40) AND (name !~ '[[:cntrl:]]'::text)))",
		want:   map[string]extent{"name": {chars: 40}},
	},
	{
		// A vocabulary CHECK whose literal holds a parenthesis and the word AND: the scan
		// that splits clauses has to see neither.
		why:    "a literal that holds a parenthesis and an AND",
		asked:  []string{"kind"},
		clause: "CHECK (((kind = ANY (ARRAY['a)b'::text, 'c AND d'::text]))))",
		want:   map[string]extent{},
	},
	{
		// A bound over a column this scrub does not ask about travels in the same text and
		// belongs to nobody here.
		why:    "a bound over a column nobody asked for",
		asked:  []string{"note"},
		clause: "CHECK ((char_length(other) <= 30))",
		want:   map[string]extent{},
	},
}

// TestWhatTheConstraintGuaranteesAndWhatItOnlyMentions asks the reader of a CHECK's own
// text for the bound each column carries in it: the ceiling an erasure has to stay inside,
// and nothing else.
//
// The direction every refusal here fails in is the one that matters: a bound that is read
// where the server stated none would cut a colleague's text down to a number the row was
// never held to, and the cut is undone by nobody.
func TestWhatTheConstraintGuaranteesAndWhatItOnlyMentions(t *testing.T) {
	for _, tc := range houseBounds {
		wanted := map[string]bool{}
		for _, column := range tc.asked {
			wanted[column] = true
		}
		got := clauseBounds(tc.clause, wanted)
		for column, want := range tc.want {
			if got[column] != want {
				t.Errorf("%s: %s bound = %+v, want %+v", tc.why, column, got[column], want)
			}
		}
		for column, room := range got {
			if _, expected := tc.want[column]; !expected {
				t.Errorf("%s: %s bound = %+v, want none", tc.why, column, room)
			}
		}
	}
}

// TestEveryPlaceholderARewriteWritesIsBoundOnce counts the question marks of the finished
// statement against the values the caller will hand the server.
//
// The rewrite is assembled from fragments, each carrying its own values, and this is the
// whole check that they arrived in the order the SQL asks for them: a value one placeholder
// away from the expression that means it either dies on a type the server will not coerce or,
// worse, runs, matches nothing, and certifies an erasure that scrubbed nothing.
func TestEveryPlaceholderARewriteWritesIsBoundOnce(t *testing.T) {
	person := Subject{ID: uuid.New(), Email: "jo.jones@example.com", DisplayName: "Jo Jones", Handle: "joj"}
	square := extent{chars: 40}
	wide := extent{chars: 262144, octets: 1048576}
	for _, tc := range []struct {
		why     string
		rooms   map[string]extent
		columns []string
		isJSON  bool
		counter string
		stamp   string
	}{
		{why: "a column with no bound at all", columns: []string{"note"}},
		{why: "a column bounded in characters", rooms: map[string]extent{"title": square}, columns: []string{"title"}},
		{why: "a column bounded in characters and bytes", rooms: map[string]extent{"body": wide}, columns: []string{"body"}},
		{why: "a column bounded in bytes alone", rooms: map[string]extent{"body": {octets: wide.octets}}, columns: []string{"body"}},
		{why: "a bounded and an unbounded column together", rooms: map[string]extent{"title": square}, columns: []string{"title", "note"}},
		{why: "a document column", rooms: map[string]extent{"payload": square}, columns: []string{"payload"}, isJSON: true},
		{why: "a counted row", rooms: map[string]extent{"title": square}, columns: []string{"title"}, counter: "revision"},
		{why: "a row that names its own instant", rooms: map[string]extent{"alt": square}, columns: []string{"alt"}, stamp: "updated_at"},
	} {
		q, err := query(person, guard{counter: tc.counter, stamp: tc.stamp, at: time.Unix(1700000000, 0)},
			"things", tc.columns, tc.isJSON, tc.rooms, "tenant_id = ?", []any{person.ID})
		if err != nil {
			t.Fatalf("%s: %v", tc.why, err)
		}
		if q == nil {
			t.Fatalf("%s: nothing to rewrite, which a person with three identifiers has", tc.why)
		}
		sql := strings.Join(q.sets, ", ") + " WHERE " + q.filter
		if n := strings.Count(sql, "?"); n != len(q.args) {
			t.Errorf("%s: %d placeholders and %d values\n%s", tc.why, n, len(q.args), sql)
		}
	}
}
