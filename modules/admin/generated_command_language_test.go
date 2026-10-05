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

func TestGeneratedCommandsUseThePagesCatalogue(t *testing.T) {
	router := mountAs(t, caller{}, func(d *admin.Deps) {
		d.Messages = xtext.Pseudo(xtext.Load("en", admin.Catalogue(), xtext.Source{Name: "resource", FS: resource.Catalogues()}), "en-XA")
	})
	code, body, location := call(t, router, http.MethodPost, "/app/note/notes", "title=Command+example&status=open")
	if code != http.StatusSeeOther {
		t.Fatalf("create = %d: %s", code, body)
	}
	req := httptest.NewRequest(http.MethodGet, "http://"+host+location, nil)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Accept-Language", "en-XA")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	body = w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `lang="en-XA"`) || !strings.Contains(body, "Command example") {
		t.Fatalf("detail = %d; expected the created row in en-XA", w.Code)
	}
	if !strings.Contains(body, `action="`+location+`/publish"`) {
		t.Fatal("the declared publish command is missing from the row")
	}
	for _, english := range []string{">Publish a note<", ">Mark as read<", `aria-label="Makes it visible."`, ">At<"} {
		if strings.Contains(body, english) {
			t.Errorf("pseudo-localized detail retains command copy %q", english)
		}
	}
}
