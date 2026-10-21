package page_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale/providers/pseudo"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/ui/legible"
	"github.com/septagon-oss/platformkit/ui/page"
)

func TestFaultReferenceCopyIsReachableThroughTheCatalogue(t *testing.T) {
	const reference = "9999aaaa-1111-2222-3333-444455556666"
	s := shell()
	s.Messages = pseudo.Wrap(xtext.Load("en", page.Catalogue()), nil)
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://demo.localhost/missing", nil)
			req.Header.Set("Accept-Language", "pt-PT")
			got := renderFault(t, s, &problem.Problem{Status: status, Instance: "urn:request:" + reference}, req)
			if got.Code != status || !strings.Contains(got.Body.String(), reference) {
				t.Fatalf("the refusal did not preserve its status and support reference: %d %s", got.Code, got.Body.String())
			}
			collected, err := legible.Scan(got.Body.Bytes(), pseudo.Wrapped)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range legible.Violations(collected) {
				if strings.Contains(s.Text, reference) {
					t.Errorf("the support reference carries untranslated copy at %s: %q", s.Path, s.Text)
				}
			}
		})
	}
}
