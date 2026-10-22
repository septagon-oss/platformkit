package page_test

// A served page that declared its subject and whose read broke on the server. The fixture
// is subjectServer: the kernel's router and guards, and this package's failure page wired
// the way an application wires it.

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// TestAFailedReadOfADeclaredSubjectSaysWhatCouldNotBeLoadedAndOffersToTryAgain: a list or a
// record whose read broke names what could not be loaded and links back to the address that
// failed, instead of the protocol's name for the verdict.
func TestAFailedReadOfADeclaredSubjectSaysWhatCouldNotBeLoadedAndOffersToTryAgain(t *testing.T) {
	var fail error
	h := subjectServer(t, &fail)
	for _, at := range []string{subjectList, subjectRecord} {
		if got := openSubject(t, h, at); got.Code != http.StatusOK {
			t.Fatalf("GET %s = %d with a read that succeeds; this case is about the read that fails", at, got.Code)
		}
	}

	fail = errors.New("the connection to the database was reset")
	for _, c := range []struct{ at, sentence string }{
		{subjectList, "We couldn&#39;t load tasks."},
		{subjectRecord, "We couldn&#39;t load this task."},
	} {
		got := openSubject(t, h, c.at)
		body := got.Body.String()
		if got.Code != http.StatusInternalServerError {
			t.Errorf("GET %s with a broken read = %d, want 500", c.at, got.Code)
		}
		for _, want := range []string{c.sentence, "Try again", `href="` + c.at + `"`} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s with a broken read omits %q\n%s", c.at, want, trim(body))
			}
		}
		for _, reject := range []string{"Internal Server Error", "connection to the database", subjectRowTitle} {
			if strings.Contains(body, reject) {
				t.Errorf("GET %s with a broken read says %q\n%s", c.at, reject, trim(body))
			}
		}
	}
}
