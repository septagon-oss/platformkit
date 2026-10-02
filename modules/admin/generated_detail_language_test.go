package admin_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/admin"
)

func TestGeneratedDetailSpeaksPortuguese(t *testing.T) {
	router := mountAs(t, caller{}, func(d *admin.Deps) {
		// The current forms use flat field keys; the specified resource keys are
		// fully scoped. Both give a corrected detail renderer copy to request.
		d.Messages = xtext.Load("en", admin.Catalogue(), xtext.Source{
			Name: "note",
			FS: fstest.MapFS{
				"en.json": {Data: []byte(`{
					"field.title":{"translation":"Title"},
					"field.status":{"translation":"Status"},
					"enum.done":{"translation":"Done"},
					"field.note.note.title":{"translation":"Title"},
					"field.note.note.status":{"translation":"Status"},
					"enum.note.note.status.done":{"translation":"Done"}
				}`)},
				"pt-PT.json": {Data: []byte(`{
					"field.title":{"translation":"Título"},
					"field.status":{"translation":"Estado"},
					"enum.done":{"translation":"Concluído"},
					"field.note.note.title":{"translation":"Título"},
					"field.note.note.status":{"translation":"Estado"},
					"enum.note.note.status.done":{"translation":"Concluído"}
				}`)},
			},
		})
	})
	code, body, location := call(t, router, http.MethodPost, "/app/note/notes", "title=First+note&status=done")
	if code != http.StatusSeeOther {
		t.Fatalf("create note = %d: %s", code, body)
	}
	req := httptest.NewRequest(http.MethodGet, "http://"+host+location, nil)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	page := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(page, `lang="pt-PT"`) || !strings.Contains(page, "First note") {
		t.Fatalf("detail did not reach the Portuguese row: status=%d body=%s", w.Code, page)
	}
	for _, english := range []string{">Title<", ">Status<", ">Done<"} {
		if strings.Contains(page, english) {
			t.Errorf("Portuguese detail retains English field copy %q", english)
		}
	}
}
