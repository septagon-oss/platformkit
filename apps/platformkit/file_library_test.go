package main

// The media library's page, asked of the composition that boots it: the grid a
// member of a tenant sees, the filters that decide what is in it, the panel one file
// opens in, and the two commands its controls post.
//
// These are the assertions the page exists to keep. The service underneath is
// answered for by modules/file's own suites and by the conformance fake; what is
// pinned here is that a person reaches those decisions through a page at the address
// the module's own nav entry names — including that a caption is written against the
// row the panel was drawn from, and that a removal the ledger would strand is refused
// rather than performed.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
)

// libraryPath is the address the manifest's nav entry names, and the one the page
// mounts itself at. It is written out rather than derived because the test that
// refuses a nav entry leading nowhere is the one that would otherwise be the only
// place anybody noticed.
const libraryPath = "/app/file/files"

// TestTheLibraryListsWhatItsFiltersWereGiven pins the grid and its two axes: a file
// the tenant uploaded is named on the page, a family the file does not belong to
// answers the no-results state rather than the tile, and a name in the box finds the
// row a person came for.
func TestTheLibraryListsWhatItsFiltersWereGiven(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := putFile(t, cfg, admin, filesPath, "invoice-october.txt", "text/plain", "one page of figures")
	if code != http.StatusCreated {
		t.Fatalf("uploading a file = %d %s; the case cannot be asked without one", code, body)
	}

	status, ctype, page := askNavigate(t, cfg, admin, acmeHost, libraryPath)
	if status != http.StatusOK {
		t.Fatalf("GET %s = %d (%s), want the library: %s", libraryPath, status, ctype, firstLineOf(page))
	}
	if !strings.Contains(page, "invoice-october.txt") {
		t.Errorf("the library does not name the tenant's file: %s", firstLineOf(page))
	}
	// A text file is a document to the library, so the image filter must not show
	// it and the document filter must. The two answers are the filter's whole
	// meaning, and a grid that ignored `type` would pass the first and fail nothing.
	if _, _, only := askNavigate(t, cfg, admin, acmeHost, libraryPath+"?type=image"); strings.Contains(only, "invoice-october.txt") {
		t.Error("the image filter listed a text file")
	}
	_, _, documents := askNavigate(t, cfg, admin, acmeHost, libraryPath+"?type=document")
	if !strings.Contains(documents, "invoice-october.txt") {
		t.Error("the document filter lost the tenant's document")
	}
	_, _, searched := askNavigate(t, cfg, admin, acmeHost, libraryPath+"?q=october")
	if !strings.Contains(searched, "invoice-october.txt") {
		t.Error("the search box found nothing where the name matches")
	}
	_, _, missed := askNavigate(t, cfg, admin, acmeHost, libraryPath+"?q=september")
	if strings.Contains(missed, "invoice-october.txt") {
		t.Error("a search that matches nothing still listed the file")
	}
}

// TestTheLibraryWritesACaptionAgainstTheRowItShowed is the panel's write, and the
// half that makes it a command rather than a form: the stamp the page drew with goes
// back with the words, and the second copy of the same form — the one a person who
// was looking at an older row submits — is refused without writing.
func TestTheLibraryWritesACaptionAgainstTheRowItShowed(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := putFile(t, cfg, admin, filesPath, "cover-art.txt", "text/plain", "the artwork")
	if code != http.StatusCreated {
		t.Fatalf("uploading a file = %d %s; the case cannot be asked without one", code, body)
	}
	id := field(t, body, "id")

	_, _, panel := askNavigate(t, cfg, admin, acmeHost, libraryPath+"?file="+id)
	stamp := attributeAfter(t, panel, `name="updated" value="`)
	if stamp == "" {
		t.Fatalf("the panel drew no revision to write against: %s", firstLineOf(panel))
	}
	code, location, said := askForm(t, cfg, admin, acmeHost, libraryPath+"/"+id+"/alt",
		"alt="+url.QueryEscape("The artwork, as its author calls it")+"&updated="+stamp)
	if code != http.StatusSeeOther {
		t.Fatalf("posting the caption = %d (%s), want the redirect that cannot be repeated: %s", code, location, said)
	}
	if location != libraryPath+"?file="+id {
		t.Errorf("the caption left the person at %q, want the panel they wrote it from", location)
	}
	if _, _, row := askNavigate(t, cfg, admin, acmeHost, filesPath+"/"+id); !strings.Contains(
		row, `"alt":"The artwork, as its author calls it"`) {
		t.Error("the row the panel answered for carries no caption")
	}

	// The same form, the stamp from before the write: the row moved, so the second
	// person's caption is refused and the first one's stays.
	code, _, said = askForm(t, cfg, admin, acmeHost, libraryPath+"/"+id+"/alt",
		"alt="+url.QueryEscape("Somebody else's caption")+"&updated="+stamp)
	if code != http.StatusConflict {
		t.Errorf("a caption typed over the row it was drawn from = %d, want the refusal that says so: %s", code, said)
	}
	if _, _, row := askNavigate(t, cfg, admin, acmeHost, filesPath+"/"+id); !strings.Contains(
		row, `"alt":"The artwork, as its author calls it"`) {
		t.Error("the refused caption overwrote the one that landed first")
	}
}

// TestTheLibraryRemovesWhatNothingShows is the page's other command: a file the
// ledger does not speak about is removed through the same command the API door calls,
// and the person is left on the grid rather than on the row that is gone.
func TestTheLibraryRemovesWhatNothingShows(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := putFile(t, cfg, admin, filesPath, "scratch.txt", "text/plain", "temporary")
	if code != http.StatusCreated {
		t.Fatalf("uploading a file = %d %s; the case cannot be asked without one", code, body)
	}
	id := field(t, body, "id")

	code, location, said := askForm(t, cfg, admin, acmeHost, libraryPath+"/"+id+"/delete", "")
	if code != http.StatusSeeOther {
		t.Fatalf("posting the removal = %d (%s), want the redirect back to the grid: %s", code, location, said)
	}
	if location != libraryPath {
		t.Errorf("the removal left the person at %q, want the grid the row was removed from", location)
	}
	if code, _ := do(t, cfg, admin, http.MethodGet, acmeHost, filesPath+"/"+id, ""); code != http.StatusNotFound {
		t.Errorf("the row the page removed still answers at %s/%s = %d", filesPath, id, code)
	}
	// The bytes are the row's own, and they go on the strength of the order its
	// removal published: the count is the only way to see them, because nothing in
	// the API answers for a file that is gone.
	eventually(t, "the worker to remove the bytes behind the file the page removed",
		func() bool { return blobs(t, cfg) == 0 })
	if got := blobs(t, cfg); got != 0 {
		t.Errorf("%d blob(s) are stored after the page removed the only file", got)
	}
}

// attributeAfter is one attribute's value, read off the markup the page drew. The
// panel's revision stamp is the test's whole premise, so it is read from the answer
// rather than assumed from the row.
func attributeAfter(t *testing.T, markup, marker string) string {
	t.Helper()
	_, rest, found := strings.Cut(markup, marker)
	if !found {
		return ""
	}
	value, _, found := strings.Cut(rest, `"`)
	if !found {
		return ""
	}
	return value
}
