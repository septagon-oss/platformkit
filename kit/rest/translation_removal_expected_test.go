package rest_test

import (
	"net/http"
	"testing"
)

func TestTranslationRemovalPassesTheCallersExpectedRevision(t *testing.T) {
	port := &recordedPort{Translations: &stubPort{}}
	router, articleID := mountedArticle(t, port)
	code, body := call(t, router, http.MethodPost, "/api/v1/articles/article/"+articleID+"/untranslate",
		`{"lang":"pt-PT","fields":["title"],"expected":{"title":7}}`)
	if code != http.StatusOK || len(port.removed) != 1 {
		t.Fatalf("remove translation = %d %s, calls %d", code, body, len(port.removed))
	}
	if got := port.removed[0].Expected["title"]; got != 7 {
		t.Errorf("removal expected revision = %d, want the caller's 7", got)
	}
}
