package rest_test

// A `?lang=` PATCH names the language its body is written in, and a language this
// tenant is never served in is the caller's mistake to fix: 422, nothing filed on
// the translation port, and the row still holding the text it held. The shape is
// worth pinning because the friendly dispatch — a language nobody speaks goes to
// the source handler instead — is one edit away, and it turns a typo in the query
// into an overwrite of the record's own columns: the one write this door exists to
// refuse. Every assertion is reached through the row, not through what a refusal
// prints, and the read-back is what a wrong dispatch cannot hide behind.

import (
	"net/http"
	"strings"
	"testing"
)

func TestATranslatablePatchInALanguageTheTenantIsNeverServedInLeavesTheRow(t *testing.T) {
	port := &recordedPort{Translations: &stubPort{}}
	router, articleID := mountedArticle(t, port)

	code, body, _ := quoted(t, router, http.MethodPatch, "/api/v1/articles/article/"+articleID+"?lang=de-DE",
		`{"title":"Nein."}`, "")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("PATCH ?lang=de-DE = %d %s, want 422", code, body)
	}
	if len(port.saved) != 0 {
		t.Fatalf("a language the tenant is never served in filed a translation: %+v", port.saved)
	}

	code, body, _ = callHead(t, router, http.MethodGet, "/api/v1/articles/article/"+articleID)
	if code != http.StatusOK || !strings.Contains(body, "About us.") {
		t.Fatalf("the row read back after the refused write above = %d %s, want 200 still holding About us.", code, body)
	}
}
