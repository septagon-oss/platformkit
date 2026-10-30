package screens_test

// A guided page is brought to life by addresses, not by names: the browser fetches the src
// and href the written document carries, and the shell answers assets at whatever prefix it
// was handed. The cases beside this one build the tag they then look for out of the same two
// names they hand the shell, so the address they check is a copy of the join they were given.
// A join that drops a slash, doubles one, or moves an address outside the tree the shell was
// given leaves a document whose every element is in place while nothing it buys answers —
// the countdown never counts and the page is unreadable without its stylesheet. So this case
// reads the addresses out of the written bytes, cuts each back to a name in the tree this
// shell serves assets at, and requires those bytes to answer: the countdown named by a page
// that runs a countdown, and the stylesheet named by every page.

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/page"
	core "github.com/septagon-oss/platformkit/ui/resource"
)

// boughtAddresses are the two addresses a person's browser is told to fetch for a guided
// page, each captured the way the document writes it, with the query the shell puts on the
// stylesheet cut off before the name is asked of the served tree.
var boughtAddresses = []struct {
	what  string
	found *regexp.Regexp
}{
	{"countdown", regexp.MustCompile(`<script src="([^"]+)" type="module">`)},
	{"stylesheet", regexp.MustCompile(`<link rel="stylesheet" href="([^"]+)"`)},
}

// TestTheGuidedDocumentBuysNoAddressItsShellDoesNotAnswer writes each guided page as the
// shell writes it and asks, of the tree that same shell serves its assets at, for every
// address the document tells the browser to fetch. An address outside that prefix, one whose
// path holds a doubled slash no asset route joins, and one the served tree answers with no
// bytes are each refused per page and per address.
func TestTheGuidedDocumentBuysNoAddressItsShellDoesNotAnswer(t *testing.T) {
	words, session := mountWords(t), oneSetRecorded()
	sheet := ui.Compose(design.Default())
	tree := ui.Assets(sheet)
	chrome := page.Chrome{Brand: "Practice", Assets: "/app/admin/assets", Stylesheet: sheet}
	for _, screen := range []struct {
		mode      string
		step, set int
	}{
		{"step", 1, 0}, {"rest", 0, 0}, {"finish", 0, 0}, {"summary", 0, 0},
	} {
		view := core.Guided(core.GuidedPage{Session: session, Policy: core.GuidedPolicy{
			Complete: core.GuidedCapability{Declared: true, Callable: true},
			Finish:   core.GuidedCapability{Declared: true, Callable: true}, Labels: words},
			Base: "/app/note/notes/" + session.ID.String(), Exit: "/app", Mode: screen.mode,
			Step: screen.step, Set: screen.set, Extra: 15},
			core.Options{})
		out := writeDocument(t, page.Document(chrome, page.Request{Path: screen.mode}, view, g.Group(view.Body)))
		for _, want := range boughtAddresses {
			found := want.found.FindStringSubmatch(out)
			if found == nil {
				t.Errorf("the %s document writes no %s address, so the person holding it is given nothing to fetch", screen.mode, want.what)
				continue
			}
			address := strings.SplitN(found[1], "?", 2)[0]
			// Cut to a name rather than test that the prefix appears in it: an address
			// the browser resolves elsewhere is an address this shell answers nothing at.
			name, ok := strings.CutPrefix(address, chrome.Assets+"/")
			if !ok {
				t.Errorf("the %s document fetches its %s at %s, outside the assets this shell answers at %s", screen.mode, want.what, address, chrome.Assets)
				continue
			}
			if strings.Contains(name, "//") {
				t.Errorf("the %s document fetches its %s at %s, whose path holds a doubled slash no asset route joins", screen.mode, want.what, address)
				continue
			}
			served, err := fs.ReadFile(tree, name)
			if err != nil {
				t.Errorf("the %s document fetches %s and this shell's assets answer %v, so the page tells the browser to buy an address nothing serves", screen.mode, address, err)
				continue
			}
			// The bytes have to be the thing the page bought rather than any file under
			// that address: the countdown names the element it installs on, and the sheet
			// carries the custom property every token this page's markup uses is defined by.
			marker := map[string]string{"countdown": "data-guided", "stylesheet": "--pk-"}[want.what]
			if !strings.Contains(string(served), marker) {
				t.Errorf("the %s document fetches %s and %d served bytes name no %s, so the page bought a file that answers it no %s", screen.mode, address, len(served), marker, want.what)
			}
		}
	}
}
