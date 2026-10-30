package screens_test

// The countdown reaches a person through three joins, and the case beside this one stops
// at the first of them: the view names js/guided.js, the shell turns that name into one
// <script type="module"> fetched under its own asset prefix, and the address it writes is
// one the same shell serves its assets at. ui/document pins the middle join for the rich
// text editor alone; the guided pages that reach a router are asked for their deadline, the
// seconds they stop adding at and the words they say, and never for what the person's
// browser is told to load. Drop the module from the view and the pause never counts; join
// the tag to the prefix the wrong way, or move that prefix under the pages mounted on it,
// and the browser is asked for a script that answers nothing — and every case that reads a
// render, a number or a word stays green, because none of them joins the two. So this case
// writes the four pages a session has as a shell writes them, and asks the served bytes for
// the one module the countdown is, at the address this shell answers assets at, beside the
// element it installs on.

import (
	"io/fs"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/page"
	core "github.com/septagon-oss/platformkit/ui/resource"
)

// guidedAssets is a shell's own asset prefix: the address its sheet, its controllers and
// its countdown are served at, which every tag a document writes is joined to.
const guidedAssets = "/app/assets"

// TestTheGuidedDocumentLoadsItsCountdownWhereTheShellServesIt writes each guided page as
// the shell writes it and requires the document to fetch the countdown once, under the
// prefix this shell serves assets at, in a document that also carries the element the
// loaded script installs on. A page that named no countdown and one that paid for a second
// module are refused per page; a shell that serves no bytes at that name is refused before
// any page is written, because then no address naming the countdown could answer a browser.
func TestTheGuidedDocumentLoadsItsCountdownWhereTheShellServesIt(t *testing.T) {
	words, session := mountWords(t), oneSetRecorded()
	sheet := ui.Compose(design.Default())
	chrome := page.Chrome{Brand: "Practice", Assets: guidedAssets, Stylesheet: sheet}
	tree := ui.Assets(sheet)
	served, err := fs.ReadFile(tree, guidedCountdown)
	if err != nil || len(served) == 0 {
		t.Fatalf("this shell serves no countdown at %s, so no address naming it can answer a browser: %v", guidedCountdown, err)
	}
	for _, screen := range []struct {
		mode      string
		step, set int
	}{
		{"step", 1, 0}, {"rest", 0, 0},
		// The two pages a session ends on, which no other case asks what they load.
		{"finish", 0, 0}, {"summary", 0, 0},
	} {
		view := core.Guided(core.GuidedPage{Session: session, Policy: core.GuidedPolicy{
			Complete: core.GuidedCapability{Declared: true, Callable: true},
			Finish:   core.GuidedCapability{Declared: true, Callable: true}, Labels: words},
			Base: "/app/note/notes/" + session.ID.String(), Exit: "/app", Mode: screen.mode,
			Step: screen.step, Set: screen.set, Extra: 15},
			core.Options{})
		// The name the view carries, joined the way the shell joins it — the one way that
		// tag is written: fetched under this shell's prefix, as a module.
		tag := `<script src="` + guidedAssets + "/" + guidedCountdown + `" type="module">`
		out := writeDocument(t, page.Document(chrome, page.Request{Path: screen.mode}, view, g.Group(view.Body)))
		if n := strings.Count(out, tag); n != 1 {
			t.Errorf("the %s document names its countdown %d time(s) at %s, want the one module tag a browser fetches", screen.mode, n, tag)
		}
		if n := strings.Count(out, `type="module"`); n != 1 {
			t.Errorf("the %s document names %d module script(s), want the countdown alone: a page pays for what it loads", screen.mode, n)
		}
		// The element the loaded countdown installs on arrived in the same answer, or the
		// one script the page bought ran over an empty page.
		if !strings.Contains(out, `data-guided="`) {
			t.Errorf("the %s document carries no element for the countdown it loads to install on", screen.mode)
		}
	}
}

// writeDocument renders one document to its bytes, and refuses a shell that cannot write it.
func writeDocument(t *testing.T, node g.Node) string {
	t.Helper()
	var out strings.Builder
	if err := node.Render(&out); err != nil {
		t.Fatalf("the shell cannot write this document: %v", err)
	}
	return out.String()
}
