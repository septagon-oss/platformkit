package file_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheGridNumbersItsTilesAgainstTheTypeOfFileItAnswers asks the library, narrowed
// to one kind of file, what it numbers its tiles against: the kind it answers with, or
// every file the tenant holds. The count rides the same filtered read as the rows, so a
// type filter that reaches the rows but not the count prints the whole library's total
// above a shorter grid and names the grid's last tile after files it never drew. Five
// files carry one name fragment — three documents and two pictures filed beside them —
// and the case reads the unfiltered page's own numbering, and which tiles it draws,
// before it asks the filtered page anything.
func TestTheGridNumbersItsTilesAgainstTheTypeOfFileItAnswers(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	frame := wideFrame(t)
	documents := make([]string, 0, 3)
	for i := range 3 {
		name := fmt.Sprintf("numbered-kind-%02d.txt", i)
		if code, out := upload(t, router, files, name, "text/plain", "one report"); code != http.StatusCreated {
			t.Fatalf("filing %s beside the pictures = %d %s", name, code, out)
		}
		documents = append(documents, name)
	}
	pictures := make([]string, 0, 2)
	for i := range 2 {
		name := fmt.Sprintf("numbered-kind-%02d.jpg", i)
		if code, out := upload(t, router, files, name, "image/jpeg", string(frame)); code != http.StatusCreated {
			t.Fatalf("filing %s beside the documents = %d %s", name, code, out)
		}
		pictures = append(pictures, name)
	}
	filed := len(documents) + len(pictures)
	narrow := url.Values{"q": {"numbered-kind"}}

	// The probe reads what a type filter cannot change before the filter is asked
	// anything: this library holds five files the fragment names, and the whole library
	// numbers its tiles against five whether or not the type reaches the count.
	whole := libraryPage(t, router, libraryAddress+"?"+narrow.Encode())
	if drawn := named(append(append([]string{}, documents...), pictures...), whole); len(drawn) != filed {
		t.Fatalf("the library named by %q drew %d of the %d files this case filed", "numbered-kind", len(drawn), filed)
	}
	if !strings.Contains(whole, fmt.Sprintf("— file %d of %d", filed, filed)) {
		t.Fatalf("the unfiltered library did not number its last tile against the %d files this case filed", filed)
	}

	only := url.Values{"q": {"numbered-kind"}, "type": {contracts.FamilyDocument}}
	filtered := libraryPage(t, router, libraryAddress+"?"+only.Encode())
	if drawn := named(documents, filtered); len(drawn) != len(documents) {
		t.Errorf("the documents-only grid drew %d of the %d documents the filter matched", len(drawn), len(documents))
	}
	if leaked := named(pictures, filtered); len(leaked) != 0 {
		t.Errorf("the documents-only grid drew the pictures its filter excludes: %v", leaked)
	}
	if want := fmt.Sprintf("— file %d of %d", len(documents), len(documents)); !strings.Contains(filtered, want) {
		t.Errorf("the documents-only grid numbered its tiles against something larger than its own match, want the last of them reading %q", want)
	}
	if against := fmt.Sprintf("— file %d of %d", len(documents), filed); strings.Contains(filtered, against) {
		t.Errorf("the documents-only grid numbered its tiles %q, against the %d files this tenant holds rather than the %d documents it answers with",
			against, filed, len(documents))
	}
}
