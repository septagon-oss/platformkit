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
	"github.com/septagon-oss/platformkit/ui/page"
)

func TestRolesRefusalsSpeakTheRequestsLanguage(t *testing.T) {
	for _, tc := range []struct {
		name, form, english string
	}{
		{"unreadable form", "name=%ZZ", "this form could not be read"},
		{"undeclared permission", "name=member&permissions=note%3Aeat", "no module defines the permission"},
		{"last administrator", "name=admin", "is the last role that grants"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := seeded()
			router := mountAs(t, caller{}, withRoles(store), func(d *admin.Deps) {
				d.Messages = xtext.Load("en", admin.Catalogue(), page.Catalogue())
			})
			req := httptest.NewRequest(http.MethodPost, "http://"+host+"/app/auth/roles", strings.NewReader(tc.form))
			req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Accept", "text/html")
			req.Header.Set("Accept-Language", "pt-PT")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("roles refusal = %d, want 422: %s", w.Code, w.Body.String())
			}
			body := w.Body.String()
			if got := regexp.MustCompile(`<html[^>]*\blang="([^"]*)"`).FindStringSubmatch(body); len(got) != 2 || got[1] != "pt-PT" {
				t.Errorf("Portuguese roles refusal declares %v, want pt-PT", got)
			}
			if strings.Contains(body, tc.english) {
				t.Errorf("Portuguese roles refusal retains English interface copy %q", tc.english)
			}
			if len(store.roles) != 2 || len(store.roles[0].Grants) != 1 || store.roles[0].Grants[0] != "*" || len(store.roles[1].Grants) != 0 {
				t.Error("a refused role change altered the seeded grants")
			}
		})
	}
}
