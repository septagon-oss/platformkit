package screens_test

// A guided page is unreadable without its stylesheet, and a person who walks a session
// step by step asks for that sheet once per page. The kernel's static rule decides what
// that costs: an address that names the bytes it points at is kept for a year and served
// from the cache, and any other address is revalidated on every page view. The decision is
// made from the query alone — `?v=` must equal the first eight bytes of the served file's
// SHA-256 — and the shell writes it from the sheet it was handed, while the bytes come
// from the tree the shell serves. The two halves are computed apart, so a composition can
// hand a page a sheet whose name no longer matches the file its own tree holds: every
// element, class and address stays in place, the sheet still renders, and the only thing a
// person notices is that each step of the walk downloads it again.
//
// The cases beside this one each hold one half: one asks whether the written document
// carries the fingerprint the chrome was given, another asks whether the served tree holds
// the sheet this composition composed, and a third reads the address out of a guided page
// and cuts its query away. Nothing asks a guided page's own stylesheet address the question
// the browser is answered by — does the query the document writes name the bytes that tree
// serves at that name. So this case reads the whole address out of the written bytes,
// including the query the others discard, and hashes the bytes the served tree answers
// with. It asks it of each page a session has, because each is a page view and a sheet a
// browser is told to keep or to fetch again.

import (
	"crypto/sha256"
	"encoding/hex"
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

// sheetBought is the stylesheet address a guided document tells the browser to fetch, with
// the path and the `?v=` the shell put on it kept apart.
var sheetBought = regexp.MustCompile(`<link rel="stylesheet" href="([^"?]+)\?v=([^"]*)"`)

// TestTheGuidedPageBuysItsSheetAtTheAddressItsBytesName writes each guided page as the
// shell writes it and asks the served tree for the stylesheet the document buys. The name
// cut from the address has to be a file the tree answers, and the query the document
// carries has to be the hash of those bytes the kernel's static rule pins the immutable
// line by. An address whose query names other bytes, an empty query, and an address whose
// name nothing serves are each refused per page.
func TestTheGuidedPageBuysItsSheetAtTheAddressItsBytesName(t *testing.T) {
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
		found := sheetBought.FindStringSubmatch(out)
		if found == nil {
			// Read the address without its query before refusing: a shell that drops the
			// query is the failure this case is for, and it should say so.
			if !strings.Contains(out, chrome.Assets+"/app.css") {
				t.Fatalf("the %s document writes no stylesheet under %s, so the person holding it reads an unstyled page", screen.mode, chrome.Assets)
			}
			t.Fatalf("the %s document fetches its stylesheet with no ?v=, so the kernel's static rule cannot keep it and every step of the walk asks for it again", screen.mode)
		}
		name, ok := strings.CutPrefix(found[1], chrome.Assets+"/")
		if !ok {
			t.Fatalf("the %s document fetches its stylesheet at %s, outside the assets this shell answers at %s", screen.mode, found[1], chrome.Assets)
		}
		served, err := fs.ReadFile(tree, name)
		if err != nil {
			t.Fatalf("the %s document fetches %s and this shell's assets answer %v", screen.mode, found[1], err)
		}
		sum := sha256.Sum256(served)
		if want := hex.EncodeToString(sum[:8]); found[2] != want {
			t.Errorf("the %s document buys %s at ?v=%s while the %d bytes this shell serves at %s hash to %s, so the browser is told to revalidate a sheet it could keep, and a sheet that changed would be bought at an address it was cached under",
				screen.mode, found[1], found[2], len(served), name, want)
		}
	}
}
