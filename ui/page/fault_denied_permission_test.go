package page_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/ui/page"
)

// TestAMissingGrantNamesThePermissionAndWhoGrantsIt: UX walkthroughs on 2026-09-30 found an editor who
// signed in and met "Não pode fazer isto." with a link back to the same refusal. The guard's detail already
// carries the permission it asked for; the page now says it and who can grant it, in the language the request
// asked for.
func TestAMissingGrantNamesThePermissionAndWhoGrantsIt(t *testing.T) {
	s := shell()
	s.Messages = xtext.Load("en", page.Catalogue())
	ask := func(language string) string {
		req := httptest.NewRequest(http.MethodGet, "http://demo.localhost/app/serviceslaw", nil)
		req.Header.Set("Accept-Language", language)
		return renderFault(t, s, &problem.Problem{
			Status: http.StatusForbidden, Detail: httpx.CodeDenied + ": this operation requires practice:edit",
		}, req).Body.String()
	}
	for language, want := range map[string][]string{
		"en":    {"You need the practice:edit permission for it.", "ask your administrator"},
		"pt-PT": {"Precisa da permissão practice:edit para isso.", "peça ao seu administrador"},
	} {
		t.Run(language, func(t *testing.T) {
			body := ask(language)
			for _, w := range want {
				if !strings.Contains(body, w) {
					t.Errorf("the refusal in %s does not say %q: %s", language, w, firstLine(body))
				}
			}
		})
	}
}
