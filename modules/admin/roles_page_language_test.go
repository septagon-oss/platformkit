package admin_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/admin"
)

func TestRolesPageSpeaksPortuguese(t *testing.T) {
	router := mountAs(t, caller{}, withRoles(seeded()), func(d *admin.Deps) {
		d.Messages = xtext.Load("en", admin.Catalogue())
	})
	req := httptest.NewRequest(http.MethodGet, "http://"+host+"/app/auth/roles", nil)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	page := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(page, `lang="pt-PT"`) {
		t.Fatalf("roles page answered %d without Portuguese: %s", w.Code, page)
	}
	for _, english := range []string{">Roles<", ">New role<", ">Create the role<"} {
		if strings.Contains(page, english) {
			t.Errorf("Portuguese roles page retains English %q", english)
		}
	}
}
