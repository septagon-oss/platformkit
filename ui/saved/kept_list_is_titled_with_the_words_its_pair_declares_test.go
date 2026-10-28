package saved

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/ui/page"
)

// The two headings this list can answer, kept apart from one another: the words one
// module declares for its own kept copies, and the generic words the page holds for
// every list whose pair is unnamed, half-named or declared by nobody.
const (
	declaredList  = "Os meus conjuntos guardados"
	genericList   = "Itens guardados"
	declaredPair  = "hints.look/outfit.saving.listTitle"
	undeclaredMod = "stock"
	undeclaredKnd = "pallet"
)

// answers is the copy a composition merges into one request's language: this page's own
// sentences, read from the catalogue it embeds, beside the saving words the pair's module
// declares. Nothing under ui/ names a module, so the words a module declares arrive here
// the way a merge order hands them to the renderer.
type answers map[string]string

func (a answers) Text(key, fallback string, args ...any) string {
	if said, ok := a[key]; ok && said != "" {
		return fmt.Sprintf(said, args...)
	}
	return fmt.Sprintf(fallback, args...)
}

// merged is the shipped catalogue rather than a paraphrase of it: the generic heading is
// read from the file this package embeds, so this case moves when the translation moves.
func merged(t *testing.T) *locale.Locale {
	t.Helper()
	raw, err := fs.ReadFile(Catalogues(), "pt-PT.json")
	if err != nil {
		t.Fatalf("this page ships no copy to read: %v", err)
	}
	var shipped map[string]struct {
		Translation string `json:"translation"`
	}
	if err := json.Unmarshal(raw, &shipped); err != nil {
		t.Fatalf("this page's copy could not be read: %v", err)
	}
	merged := answers{declaredPair: declaredList}
	for key, entry := range shipped {
		merged[key] = entry.Translation
	}
	return &locale.Locale{Language: "pt-PT", Formatter: merged}
}

// TestTheKeptListIsTitledWithTheWordsItsPairDeclares asks what the list of kept copies is
// called. A pair whose two halves are named is titled with the words the declaration of
// that pair carries — the same words a record's own control reads, so the button and the
// list over it cannot drift apart — and every other list takes the generic words: no pair
// named, half of one named, and a pair no module declares at all. The second of those is
// the whole point of the heading: two kinds called `order` in two modules would otherwise
// share one heading, and a row of a pair whose module stopped declaring its words must
// answer a heading rather than nothing. Both the page's title and the heading it draws are
// asked, because a person reads the one and a browser history holds the other.
func TestTheKeptListIsTitledWithTheWordsItsPairDeclares(t *testing.T) {
	l := merged(t)
	at := At{List: "/app/keeping/saved", Save: "/app/keeping/saved/save", Remove: "/app/keeping/saved"}
	for _, ask := range []struct {
		name string
		in   filters
		want string
	}{
		{name: "both halves named", in: filters{Module: "look", Kind: "outfit"}, want: declaredList},
		{name: "no pair named", in: filters{}, want: genericList},
		{name: "only the module named", in: filters{Module: "look"}, want: genericList},
		{name: "only the kind named", in: filters{Kind: "outfit"}, want: genericList},
		{name: "a pair nobody declares", in: filters{Module: undeclaredMod, Kind: undeclaredKnd}, want: genericList},
	} {
		view := grid(page.Request{Locale: l}, page.Shell{BackLabel: "Workspace"}, Options{Workspace: "/app"},
			at, keptCopies(2, 2), ask.in, defaultPerPage)
		if view.Status != http.StatusOK {
			t.Errorf("%s: the kept list answered %d, not its page of copies", ask.name, view.Status)
			continue
		}
		var drawn strings.Builder
		for _, n := range view.Body {
			if err := n.Render(&drawn); err != nil {
				t.Fatalf("%s: the kept list could not be rendered: %v", ask.name, err)
			}
		}
		heading := drawn.String()
		if view.Title != ask.want {
			t.Errorf("%s: the kept list is titled %q on the page and %q in the browser history",
				ask.name, ask.want, view.Title)
		}
		if !strings.Contains(heading, ask.want) {
			t.Errorf("%s: the kept list draws no heading %q\n%s", ask.name, ask.want, heading)
		}
		for _, other := range []string{declaredList, genericList} {
			if other == ask.want {
				continue
			}
			if strings.Contains(heading, other) {
				t.Errorf("%s: the kept list draws the other list's heading %q beside %q", ask.name, other, ask.want)
			}
		}
	}
}
