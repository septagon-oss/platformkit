package admin_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestBooleanDetailValuesSpeakPortuguese(t *testing.T) {
	router := mountAs(t, caller{}, func(d *admin.Deps) {
		d.Messages = xtext.Load("en", admin.Catalogue(), xtext.Source{Name: "resource", FS: resource.Catalogues()})
	})
	for _, tt := range []struct{ value, want string }{{"true", "Sim"}, {"false", "Não"}} {
		t.Run(tt.value, func(t *testing.T) {
			code, body, location := call(t, router, http.MethodPost, "/app/note/notes", "title=Language+example&status=done&pinned="+tt.value)
			if code != http.StatusSeeOther {
				t.Fatalf("create = %d: %s", code, body)
			}
			req := httptest.NewRequest(http.MethodGet, "http://"+host+location, nil)
			req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
			req.Header.Set("Accept-Language", "pt-PT")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			body = w.Body.String()
			if w.Code != http.StatusOK || !strings.Contains(body, "Language example") || !strings.Contains(body, `lang="pt-PT"`) {
				t.Fatalf("detail = %d: %s", w.Code, body)
			}
			if !strings.Contains(body, ">"+tt.want+"<") || strings.Contains(body, ">Yes<") || strings.Contains(body, ">No<") {
				t.Errorf("boolean %s lacks Portuguese %q in generated detail", tt.value, tt.want)
			}
		})
	}
}
