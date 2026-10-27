package rest_test

// A translatable PATCH dispatches on the language it quotes: only a `?lang=` naming a
// language other than the tenant's own is a translation request, and a body that quotes
// the tenant's own language edits the row's source columns like any other PATCH. That
// half of the dispatch is what lets a caller repeat the row's own language in the query
// and still write the row: send it to the translation leg and the write comes back
// refused as one. The case judges the answer through the row it moved — 200 carrying the
// new title, the translation port never reached, the same address holding it after.

import (
	"net/http"
	"strings"
	"testing"
)

func TestATranslatablePatchQuotingTheTenantsOwnLanguageEditsTheRowAndFilesNoTranslation(t *testing.T) {
	port := &recordedPort{Translations: &stubPort{}}
	router, articleID := mountedArticle(t, port)
	const renamed = `"title":"renamed by a same-language body"`

	code, body, _ := quoted(t, router, http.MethodPatch, "/api/v1/articles/article/"+articleID+"?lang=en",
		`{"title":"renamed by a same-language body"}`, "")
	if code != http.StatusOK || !strings.Contains(body, renamed) {
		t.Fatalf("PATCH the row quoting its own language = %d %s, want 200 with the row it wrote in it", code, body)
	}
	if len(port.saved) != 0 {
		t.Fatalf("a body quoting the tenant's own language filed a translation instead of editing the row's source columns: %+v", port.saved)
	}

	code, body, _ = callHead(t, router, http.MethodGet, "/api/v1/articles/article/"+articleID)
	if code != http.StatusOK || !strings.Contains(body, renamed) {
		t.Fatalf("the row read back after the write above = %d %s, want 200 carrying %s", code, body, renamed)
	}
}
