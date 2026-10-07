package legible_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale/providers/pseudo"
	"github.com/septagon-oss/platformkit/ui/legible"
)

// The glyph that joins a page's own copy to a value the installation holds is the same
// glyph a sentence sometimes holds between its own words, and only the mark tells the
// two apart. A sentence whose title carries a separator of its own is one marked
// string: the words around the separator reached a catalogue together, and the number
// may not read half of it back as copy nobody sent.
func TestAMarkedSentenceKeepsTheSeparatorInsideIt(t *testing.T) {
	const tenant = "Acme Corporation"
	for _, sentence := range []string{
		"New task — urgent", "Save, then close", "Yes | No", "18–24 months", "Rooms 3/4",
	} {
		t.Run(sentence, func(t *testing.T) {
			title := pseudo.Mark(sentence) + " · " + tenant
			collected, err := legible.Scan([]byte("<title>"+title+"</title>"), pseudo.Wrapped)
			if err != nil {
				t.Fatal(err)
			}
			if len(collected) != 1 {
				t.Fatalf("the join did not reach the scan as one node: %+v", collected)
			}
			marked := legible.MarkDatum(collected, pseudo.Wrapped, []string{tenant})
			unreachable, declined := legible.Report("GET /tasks/new", marked)
			if len(unreachable) != 0 {
				t.Errorf("a marked sentence lost its copy at the join: %s", strings.Join(unreachable, "\n"))
			}
			if len(declined) != 0 {
				t.Errorf("reached copy also counted as data: %s", strings.Join(declined, "\n"))
			}
		})
	}
}

// The same split seen from the other side: a piece of a join the gate cannot account
// for is the copy this application never sent, however much marked copy stands beside
// it. A rule that merged every piece it could would read `New task — urgent` joined
// with a stranger's name as one reached sentence.
func TestAnUnmarkedHalfOfAJoinStaysCopy(t *testing.T) {
	for _, tc := range []struct{ title, half string }{
		{"New task — urgent · Tenant Name", "New task — urgent"},
		{"⟦Save⟧ · Keep this out", "Keep this out"},
	} {
		t.Run(tc.title, func(t *testing.T) {
			collected, err := legible.Scan([]byte("<title>"+tc.title+"</title>"), pseudo.Wrapped)
			if err != nil {
				t.Fatal(err)
			}
			unreachable, _ := legible.Report("GET /tasks/new",
				legible.MarkDatum(collected, pseudo.Wrapped, []string{"Tenant Name"}))
			if len(unreachable) != 1 || !strings.Contains(unreachable[0], tc.half) {
				t.Fatalf("the unmarked half of the join is not named as copy: %v", unreachable)
			}
		})
	}
}
