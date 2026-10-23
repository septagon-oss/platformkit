package internal

// footer_scope_folded_test.go pins the footer's split: the build stamp is drawn
// once and in view, and the tenant scope is drawn once, only inside the disclosure.

import (
	"strings"
	"testing"
)

func TestTheFooterDrawsTheStampOnceAndTheScopeOnlyInsideTheDisclosure(t *testing.T) {
	for _, signedIn := range []bool{true, false} {
		footer := frameRegion(t, chromeFrameWith(t, signedIn, peopleDouble{person: Person{DisplayName: "Ada Lovelace"}}), "footer")
		begin, end := strings.Index(footer, "<details"), strings.Index(footer, "</details>")
		if begin < 0 || end < begin {
			t.Fatalf("signed in=%v: no disclosure in the footer:\n%s", signedIn, footer)
		}
		if n := strings.Count(footer, brand+" "); n != 1 {
			t.Errorf("signed in=%v: the footer draws the build stamp %d times, want 1", signedIn, n)
		}
		if n := strings.Count(footer, "Tenant workspace"); n != 1 || !strings.Contains(footer[begin:end], "Tenant workspace") {
			t.Errorf("signed in=%v: the scope is drawn %d times or outside the disclosure:\n%s", signedIn, n, footer)
		}
	}
}
