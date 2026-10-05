package page_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/ui/page"
)

func TestPublicFaultDoesNotRenameTheSiteAsTheWorkspace(t *testing.T) {
	s := shell()
	// These are the public shell's values in modules/web/internal/mount.go.
	s.Back, s.BackLabel = "/", "Back to the site"
	s.Messages = xtext.Load("en", page.Catalogue())
	for _, language := range []string{"en", "pt-PT"} {
		t.Run(language, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://demo.localhost/missing", nil)
			req.Header.Set("Accept-Language", language)
			got := renderFault(t, s, &problem.Problem{Status: http.StatusNotFound}, req)
			body := got.Body.String()
			if got.Code != http.StatusNotFound || !strings.Contains(body, `href="/"`) {
				t.Fatalf("the public refusal lost its status or link to the site: %d %s", got.Code, body)
			}
			if strings.Contains(body, "Voltar ao espaço de trabalho") {
				t.Error("the shell links to the public site but the catalogue calls that destination the workspace")
			}
		})
	}
}
