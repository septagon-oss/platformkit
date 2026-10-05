package admin_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
	"github.com/septagon-oss/platformkit/ui/page"
)

func TestAccountFormErrorsKeepPortugueseThroughoutThePage(t *testing.T) {
	router := mountAs(t, caller{}, func(d *admin.Deps) {
		d.Language = accountUsers{usertest.NewFake()}
		d.Messages = xtext.Load("en", admin.Catalogue(), page.Catalogue())
	})
	for _, tc := range []struct{ name, body string }{
		{"unsupported language", "locale=de"},
		{"malformed form", "locale=%ZZ"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://"+host+"/app/admin/language", strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Accept", "text/html")
			req.Header.Set("Accept-Language", "pt-PT")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("form refusal = %d, want 422", w.Code)
			}
			body := w.Body.String()
			language := regexp.MustCompile(`lang="[^"]*"`).FindString(body)
			if language != `lang="pt-PT"` {
				t.Errorf("Portuguese form refusal declares %s", language)
			}
			for _, english := range []string{"Unprocessable Entity", "this form could not be read", ">Dashboard<"} {
				if strings.Contains(body, english) {
					t.Errorf("Portuguese form refusal contains English interface copy %q", english)
				}
			}
		})
	}
}
