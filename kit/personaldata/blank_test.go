package personaldata

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestABlankedColumnIsAskedAboutTheWayAScrubbedOneIs checks that the two statements agree
// on which rows name the subject, which is the only thing that makes them two answers to
// one question rather than two questions.
//
// The blank's SET list is its own and its filter must not be: a blank that asked a
// narrower question would empty some of the rows and certify the rest anonymous without
// looking at them, and no re-read asks about a value that is already empty, so nothing
// downstream would ever see the difference.
func TestABlankedColumnIsAskedAboutTheWayAScrubbedOneIs(t *testing.T) {
	person := Subject{ID: uuid.New(), Email: "jo.jones@example.com", DisplayName: "Jo Jones", Handle: "joj"}
	for _, tc := range []struct {
		why     string
		columns []string
		where   string
		args    []any
	}{
		{why: "one column and no filter", columns: []string{"external_id"}},
		{why: "two columns", columns: []string{"external_id", "nickname"}},
		{why: "a caller's own filter", columns: []string{"external_id"}, where: "user_id = ?", args: []any{uuid.New()}},
	} {
		rewritten, err := query(person, guard{}, "scim_user_external", tc.columns, false, nil, tc.where, tc.args)
		if err != nil {
			t.Fatalf("%s: the scrub: %v", tc.why, err)
		}
		if rewritten == nil {
			t.Fatalf("%s: the scrub found nothing to rewrite, which this person has three strings for", tc.why)
		}
		toBlank, err := blanking(person, "scim_user_external", tc.columns, tc.where, tc.args...)
		if err != nil {
			t.Fatalf("%s: the blank: %v", tc.why, err)
		}
		if toBlank == nil {
			t.Fatalf("%s: the blank wrote no statement over a person with three identifiers", tc.why)
		}
		if rewritten.filter != toBlank.filter {
			t.Errorf("%s: the scrub asks\n%s\nand the blank asks\n%s", tc.why, rewritten.filter, toBlank.filter)
		}
		for _, c := range tc.columns {
			if want := c + " = ''"; !strings.Contains(strings.Join(toBlank.sets, ", "), want) {
				t.Errorf("%s: the blank writes %q, which does not carry %q", tc.why, toBlank.sets, want)
			}
		}
		sql := strings.Join(toBlank.sets, ", ") + " WHERE " + toBlank.filter
		if n := strings.Count(sql, "?"); n != len(toBlank.args) {
			t.Errorf("%s: %d placeholders and %d values\n%s", tc.why, n, len(toBlank.args), sql)
		}
	}
}

// TestABlankWithNothingToFindWritesNoStatement checks the one case where a statement over a
// tenant's whole table would be the largest damage it could do: a person whose row carried
// no address, no name and no handle has no string to look for, and a WHERE built from no
// terms answers for every row.
//
// The scrub refuses that person by writing nothing; so must the blank, whose value goes
// whole rather than span by span, and which would otherwise empty the column of every row
// of the tenant in an erasure that had nothing to erase.
func TestABlankWithNothingToFindWritesNoStatement(t *testing.T) {
	id := uuid.New()
	for _, tc := range []struct {
		why     string
		s       Subject
		columns []string
	}{
		{why: "a person with no identifiers", s: Subject{ID: id}, columns: []string{"external_id"}},
		{why: "identifiers that are only spaces",
			s: Subject{ID: id, Email: "  ", DisplayName: "\t"}, columns: []string{"external_id"}},
		{why: "no column asked about", s: Subject{ID: id, Email: "jo@example.com"}},
	} {
		q, err := blanking(tc.s, "scim_user_external", tc.columns, "", nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.why, err)
		}
		if q != nil {
			t.Errorf("%s: a statement to write: SET %s WHERE %s", tc.why, strings.Join(q.sets, ", "), q.filter)
		}
	}
}
