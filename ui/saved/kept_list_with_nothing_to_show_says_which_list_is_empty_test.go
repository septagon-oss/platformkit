package saved

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/page"
)

// noSuchSentence is what the shipped catalogue answers with when it holds no copy for a
// key at all: a case that compared against an empty string would pass on a page that
// says nothing, so every sentence this case asks is refused before it is asked.
const noSuchSentence = "\x00no such sentence"

// TestTheKeptListWithNothingToShowSaysWhichListIsEmpty asks the sentence over an empty
// kept list, which is the only copy on this page that states a fact about how many
// copies a person holds. A list filtered to one pair and a list of everything can both
// answer with nothing to show, and they are different answers: an emptied pair says
// nothing about the rest, so the sentence for "you have kept nothing" beside a pair the
// person arrived at from a record would tell them their whole kept list is empty when it
// is one pair they emptied. Both sentences are read from the catalogue this page embeds,
// so this case moves when a shipped translation moves rather than paraphrasing it.
func TestTheKeptListWithNothingToShowSaysWhichListIsEmpty(t *testing.T) {
	l := merged(t)
	emptiedAll := say(l, "saved.empty", noSuchSentence)
	emptiedPair := say(l, "saved.empty.filtered", noSuchSentence)
	for label, said := range map[string]string{"everything": emptiedAll, "one pair": emptiedPair} {
		if said == noSuchSentence {
			t.Fatalf("this page ships no sentence for the list that emptied %s", label)
		}
	}
	if emptiedAll == emptiedPair {
		t.Fatalf("this page says %q whether the person emptied one pair or everything", emptiedAll)
	}

	at := At{List: "/app/keeping/saved", Save: "/app/keeping/saved/save", Remove: "/app/keeping/saved"}
	for _, ask := range []struct {
		name  string
		in    filters
		said  string
		other string
	}{
		{name: "the pair they filtered by", in: filters{Module: "look", Kind: "outfit"}, said: emptiedPair, other: emptiedAll},
		{name: "everything they kept", in: filters{}, said: emptiedAll, other: emptiedPair},
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
		said := drawn.String()
		if !strings.Contains(said, ask.said) {
			t.Errorf("%s: the empty kept list says nothing about which list is empty, want %q\n%s",
				ask.name, ask.said, said)
		}
		if strings.Contains(said, ask.other) {
			t.Errorf("%s: the empty kept list says %q, the sentence for the other list\n%s",
				ask.name, ask.other, said)
		}
	}
}
