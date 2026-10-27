package file_test

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// tileNumbering matches the line one tile of the grid numbers itself with, and
// lastTileNumbering answers what the last tile on a page numbered itself with — the
// figure a person reads as the size of the match.
var tileNumbering = regexp.MustCompile(`— file \d+ of \d+`)

func lastTileNumbering(markup string) string {
	all := tileNumbering.FindAllString(markup, -1)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

// TestTheGridNumbersItsTilesAgainstThePicturesItDrew asks the library, narrowed to
// pictures, whether it numbers its tiles against the pictures it answers with. Pictures
// are the one family whose filter names a media type this deployment renders inline and a
// frame the image pass measured, so a count built for one family and not the others prints
// a total above the tiles it drew and names its last tile after a file the grid hides. This
// is the only case that asks the grid for pictures at all: every type filter asked so far
// names documents. Three files carry one name fragment — two frames and one plain text note
// — and the case reads the unfiltered page's own numbering, which no family filter can
// change, before it narrows to pictures.
func TestTheGridNumbersItsTilesAgainstThePicturesItDrew(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	frame := wideFrame(t)
	filed := make([]string, 0, 3)
	for i := range 2 {
		name := fmt.Sprintf("counted-picture-%02d.jpg", i)
		if code, out := upload(t, router, files, name, "image/jpeg", string(frame)); code != http.StatusCreated {
			t.Fatalf("filing %s for the grid = %d %s", name, code, out)
		}
		filed = append(filed, name)
	}
	note := "counted-picture-note.txt"
	if code, out := upload(t, router, files, note, "text/plain", "a note beside the pictures"); code != http.StatusCreated {
		t.Fatalf("filing the note beside the pictures = %d %s", code, out)
	}
	filed = append(filed, note)
	narrow := url.Values{"q": {"counted-picture"}}

	// The probe reads the whole library first: three files carry the fragment, and the
	// unfiltered grid numbers its last tile against three whatever a family filter does.
	whole := libraryPage(t, router, libraryAddress+"?"+narrow.Encode())
	if drew := named(filed, whole); len(drew) != len(filed) {
		t.Fatalf("the library named by %q drew %d of the %d files this case filed", "counted-picture", len(drew), len(filed))
	}
	if last := lastTileNumbering(whole); last != "— file 3 of 3" {
		t.Fatalf("the unfiltered library numbered its last tile %q, want it against the 3 files this case filed", last)
	}

	only := url.Values{"q": {"counted-picture"}, "type": {contracts.FamilyImage}}
	filtered := libraryPage(t, router, libraryAddress+"?"+only.Encode())
	if drew := named(filed[:2], filtered); len(drew) != 2 {
		t.Errorf("the pictures-only grid drew %d of the 2 pictures it matches", len(drew))
	}
	if leaked := named(filed[2:], filtered); len(leaked) != 0 {
		t.Errorf("the pictures-only grid drew the note its filter excludes: %v", leaked)
	}
	if last := lastTileNumbering(filtered); last != "— file 2 of 2" {
		t.Errorf("the pictures-only grid numbered its tiles %q, want the last of them reading %q", last, "— file 2 of 2")
	}
}
