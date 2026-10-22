package screens_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// TestAFormWhoseWriteFailedOnTheServerShowsNoneOfTheServersError: a create that failed on the
// server — an error kit/rest passes through unmapped — redraws the form with the typed value
// and a notice, and the notice carries nothing of the error the server logged. The fixture is
// formServer: the kernel's router and guards and the screens Mount draws.
func TestAFormWhoseWriteFailedOnTheServerShowsNoneOfTheServersError(t *testing.T) {
	var refuse error
	h := formServer(t, &refuse)
	if got := submitNote(t, h, "Call the harbour"); got.Code != http.StatusSeeOther {
		t.Fatalf("a create that succeeds = %d; this case is about the one that fails: %s", got.Code, got.Body.String())
	}

	refuse = errors.New("pq: could not serialize access due to concurrent update (SQLSTATE 40001)")
	body := submitNote(t, h, "Call the harbour").Body.String()
	if !strings.Contains(body, `value="Call the harbour"`) || !strings.Contains(body, "data-alert-tone") {
		t.Fatalf("a form whose write failed lost its notice or the typed value: %s", body)
	}
	for _, reject := range []string{"pq:", "SQLSTATE", "serialize access"} {
		if strings.Contains(body, reject) {
			t.Errorf("a form whose write failed on the server shows the person %q", reject)
		}
	}
}
