package saved

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/page"
)

// TestTheKeptListWithNoCopiesYetIsTitledWithTheWordsItsPairDeclares asks what the
// list is called before it holds anything. The heading a person filtered by is the
// answer they were given on the record they kept from, so an empty list keeps those
// words: a page that fell back to the generic heading the moment a pair's copies ran
// out would tell the person who removed their last copy that they were in a different
// list than the one they left, and the record's own link still names the pair. The
// empty list is the only arm that draws no card at all, so it is where a heading read
// out of the rows rather than out of the filter would show as no heading.
func TestTheKeptListWithNoCopiesYetIsTitledWithTheWordsItsPairDeclares(t *testing.T) {
	l := merged(t)
	at := At{List: "/app/keeping/saved", Save: "/app/keeping/saved/save", Remove: "/app/keeping/saved"}
	for _, ask := range []struct {
		name string
		in   filters
		want string
	}{
		{name: "a pair the person filtered by", in: filters{Module: "look", Kind: "outfit"}, want: declaredList},
		{name: "everything they kept", in: filters{}, want: genericList},
	} {
		view := grid(page.Request{Locale: l}, page.Shell{BackLabel: "Workspace"}, Options{Workspace: "/app"},
			at, Page{Total: 0}, ask.in, defaultPerPage)
		if view.Status != http.StatusOK {
			t.Errorf("%s: the empty kept list answered %d, not its page", ask.name, view.Status)
			continue
		}
		var drawn strings.Builder
		for _, n := range view.Body {
			if err := n.Render(&drawn); err != nil {
				t.Fatalf("%s: the empty kept list could not be rendered: %v", ask.name, err)
			}
		}
		heading := drawn.String()
		if view.Title != ask.want {
			t.Errorf("%s: the empty kept list is titled %q in the browser history, want %q",
				ask.name, view.Title, ask.want)
		}
		if !strings.Contains(heading, ask.want) {
			t.Errorf("%s: the empty kept list draws no heading %q\n%s", ask.name, ask.want, heading)
		}
		other := declaredList
		if ask.want == declaredList {
			other = genericList
		}
		if strings.Contains(heading, other) {
			t.Errorf("%s: the empty kept list draws the other list's heading %q beside %q", ask.name, other, ask.want)
		}
	}
}
