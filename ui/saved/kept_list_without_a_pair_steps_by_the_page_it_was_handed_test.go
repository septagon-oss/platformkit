package saved

import (
	"net/http"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/ui/page"
)

// handedPerPage is a page size a composition hands Mount through Options.PerPage. It is
// neither defaultPerPage, the page this package falls back to when none is named, nor the
// rows an answer carries, so a step drawn from the fallback, from the rows and from this
// figure are three different addresses.
const handedPerPage = 5

// TestTheKeptListWithoutAPairStepsByThePageItWasHanded asks the kept list a person lands
// on — every copy they kept, no pair named — at the page it was handed rather than the one
// it falls back to. gridAt spells that list's addresses on a branch of its own, and every
// case already asking that branch asks it at a page size equal to defaultPerPage, so there
// nothing separates the composition's page from the fallback: a pager stepping by
// defaultPerPage while its caller asked for a smaller page walked a person past their own
// copies and answered ok. Asked of forty copies three pages in, one copy in and a page
// answered short, at offsets and row counts kept apart from each other and from both page
// sizes, each arm holding its status and the exact addresses beside it.
func TestTheKeptListWithoutAPairStepsByThePageItWasHanded(t *testing.T) {
	at := At{List: "/app/keeping/saved", Save: "/app/keeping/saved/save", Remove: "/app/keeping/saved"}
	for _, ask := range []struct {
		name  string
		in    filters
		rows  int
		total int64
		want  []string
	}{
		{name: "three pages in, a page back is a page", in: filters{Offset: 15},
			rows: 5, total: 40, want: []string{at.List + "?offset=10", at.List + "?offset=20"}},
		{name: "one copy in, with a page behind it and one ahead", in: filters{Offset: 1},
			rows: 5, total: 40, want: []string{at.List + "?offset=0", at.List + "?offset=6"}},
		{name: "a page answered short, with copies still behind", in: filters{Offset: 33},
			rows: 2, total: 40, want: []string{at.List + "?offset=28", at.List + "?offset=38"}},
	} {
		view := grid(page.Request{}, page.Shell{BackLabel: "Workspace"}, Options{Workspace: "/app"},
			at, keptCopies(ask.rows, ask.total), ask.in, handedPerPage)
		if view.Status != http.StatusOK {
			t.Errorf("%s: the kept list answered %d, not the page of copies it draws", ask.name, view.Status)
			continue
		}
		if got := pagerHrefs(t, view.Body); !slices.Equal(got, ask.want) {
			t.Errorf("%s: the kept list offered %v, want %v", ask.name, got, ask.want)
		}
	}
}
