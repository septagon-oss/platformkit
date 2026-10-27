package rest_test

// A translatable PATCH door has two halves. With `?lang=` naming a language other
// than the tenant's own the body files a translation; with none, or with the
// tenant's own, the same address edits the row's source columns through a handler
// of its own, whose answer is the row. A row whose table keeps no write count has
// no count to answer, so the accepted source edit answers no tag: the number it
// would hand back is one this resource refuses to be quoted back, and a caller
// handed it is refused its next write forever. The counted half of this door is
// pinned where the count is; nothing pinned the row nobody counts, and the case
// tells the two halves apart by the port the write did not reach.
//
// Each assertion is reached through the row the write moved: its own new title in
// the answer, at 200, and read back again at the same address.

import (
	"net/http"
	"strings"
	"testing"
)

func TestTheSourceEditThroughATranslatableDoorAnswersNoTagToARowThatKeepsNoWriteCount(t *testing.T) {
	port := &recordedPort{Translations: &stubPort{}}
	router, articleID := mountedArticle(t, port)
	const renamed = `"title":"renamed by the source edit"`

	code, body, head := quoted(t, router, http.MethodPatch, "/api/v1/articles/article/"+articleID,
		`{"title":"renamed by the source edit"}`, "")
	if code != http.StatusOK || !strings.Contains(body, renamed) {
		t.Fatalf("PATCH the row's own source, quoting nothing = %d %s, want 200 with the row it wrote in it", code, body)
	}
	if tag := head.Get("ETag"); tag != "" {
		t.Errorf("the accepted source edit of a row whose table keeps no write count answered ETag %q, want no tag at all: a value this resource will refuse to be quoted back is not a tag", tag)
	}
	if len(port.saved) != 0 {
		t.Fatalf("the write filed a translation instead of editing the row's source columns: %+v", port.saved)
	}

	code, body, _ = callHead(t, router, http.MethodGet, "/api/v1/articles/article/"+articleID)
	if code != http.StatusOK || !strings.Contains(body, renamed) {
		t.Fatalf("the row read back after the write above = %d %s, want 200 carrying %s", code, body, renamed)
	}
}
