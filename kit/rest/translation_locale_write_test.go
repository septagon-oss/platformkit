package rest_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestWritingAnotherLocaleDoesNotOverwriteTheSource(t *testing.T) {
	port := &recordedPort{Translations: &stubPort{}}
	router, articleID := mountedArticle(t, port)
	path := "/api/v1/articles/article/" + articleID
	code, body := call(t, router, http.MethodPatch, path+"?lang=pt-PT", `{"title":"Sobre nós."}`)
	if code != http.StatusOK {
		t.Fatalf("write Portuguese = %d %s, want 200", code, body)
	}
	code, body = call(t, router, http.MethodGet, path+"?lang=en", "")
	if code != http.StatusOK || !strings.Contains(body, `"title":"About us."`) {
		t.Errorf("a Portuguese write overwrote the English source: %d %s", code, body)
	}
	if len(port.saved) != 1 || port.saved[0].Locale != "pt-PT" || port.saved[0].Values["title"] != "Sobre nós." {
		t.Errorf("Portuguese was not written as a translation: %+v", port.saved)
	}
}
