package admin_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/admin"
)

// The gallery renderer owns these keys; the composition supplies its source
// beside the admin source, just as an application does for every UI layer.
func galleryMessages() xtext.Catalog {
	return xtext.Load("en", admin.Catalogue(), xtext.Source{
		Name: "ui/export",
		FS: fstest.MapFS{
			"en.json": {Data: []byte(`{
				"gallery.landmark":{"translation":"Component examples"},
				"gallery.preview_theme":{"translation":"Preview theme"},
				"gallery.go_properties":{"translation":"Go properties"},
				"gallery.apply":{"translation":"Apply preview"}
			}`)},
			"pt-PT.json": {Data: []byte(`{
				"gallery.landmark":{"translation":"Exemplos de componentes"},
				"gallery.preview_theme":{"translation":"Tema da pré-visualização"},
				"gallery.go_properties":{"translation":"Propriedades Go"},
				"gallery.apply":{"translation":"Aplicar pré-visualização"}
			}`)},
		},
	})
}

func TestGalleryControlsSpeakPortuguese(t *testing.T) {
	router := mountAs(t, caller{}, func(d *admin.Deps) {
		d.Messages = galleryMessages()
	})
	req := httptest.NewRequest(http.MethodGet, "http://"+operatorHost+"/app/admin/_gallery", nil)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	page := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(page, `lang="pt-PT"`) || !strings.Contains(page, ">Componentes<") {
		t.Fatalf("gallery did not reach its Portuguese page: status=%d body=%s", w.Code, page)
	}
	for _, english := range []string{
		`aria-label="Component examples"`,
		">Preview theme<",
		">Go properties<",
		">Apply preview<",
	} {
		if strings.Contains(page, english) {
			t.Errorf("Portuguese gallery retains English control %q", english)
		}
	}
}

func TestGalleryPreviewContextSpeaksPortuguese(t *testing.T) {
	router := mountAs(t, caller{}, func(d *admin.Deps) {
		d.Messages = galleryMessages()
	})
	req := httptest.NewRequest(http.MethodGet,
		"http://"+operatorHost+"/app/admin/_gallery/preview?example=pk-ui.component.skiplink/default", nil)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	page := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("gallery preview was not reachable: status=%d body=%s", w.Code, page)
	}
	if !regexp.MustCompile(`<html\b[^>]*\blang="pt-PT"`).MatchString(page) {
		t.Error("gallery preview does not declare the requested Portuguese language")
	}
	for _, english := range []string{
		"Skip links appear when focused.",
		">Focus skip link<",
		`aria-label="Sample navigation"`,
	} {
		if strings.Contains(page, english) {
			t.Errorf("Portuguese gallery preview retains English context %q", english)
		}
	}
}
