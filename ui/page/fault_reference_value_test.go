package page_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/problem"
)

func TestAFiveHundredShowsItsReferenceAsAValueAndLeaksNoDetail(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://demo.localhost/admin", nil)
	got := renderFault(t, shell(), &problem.Problem{
		Status:     http.StatusInternalServerError,
		Diagnostic: true,
		Detail:     "pq: relation \"secret_table\" does not exist",
		Instance:   "urn:request:feedface-0000-1111-2222-333344445555",
	}, req)

	body := got.Body.String()
	if !strings.Contains(body, "feedface-0000-1111-2222-333344445555") || !strings.Contains(body, "<samp") {
		t.Errorf("the reference is not a value in a samp: %s", trim(body))
	}
	if strings.Contains(body, "secret_table") {
		t.Errorf("the page leaks the fault's detail: %s", trim(body))
	}
}
