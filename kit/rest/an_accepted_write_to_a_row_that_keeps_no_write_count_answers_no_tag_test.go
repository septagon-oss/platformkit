package rest_test

// An accepted write answers the row's own write count, and a row nobody counts
// has none to answer.
//
// A read of such a row sends no tag, which is why a quoted tag against it is 422
// and not a condition check. The write's own response answers in a tag too — an
// accepted write of a counted row hands back the count it just produced, which is
// what the next write quotes — so the same silence belongs at both doors that
// carry one: the plain PATCH a page sends, and the `?lang=` PATCH of a
// translatable entity whose table keeps no count. A write that started answering
// a count here would hand the caller a value the same resource refuses to accept
// back, with every counted row's write still answering its own beside it. Each
// leg reaches its assertion through the row the write changed: the row's own new
// name in the answer, at 200.

import (
	"net/http"
	"strings"
	"testing"
)

func TestAnAcceptedWriteToARowThatKeepsNoWriteCountAnswersNoTag(t *testing.T) {
	_, router, admin := sharedCatalogue(t, true)
	code, body := call(t, router, http.MethodPost, "/api/v1/billing/plans", `{"name":"Team","cents":4000}`)
	if code != http.StatusCreated {
		t.Fatalf("POST the catalogue row = %d %s, want 201", code, body)
	}
	key := id(t, body)

	const rewritten = `"name":"rewritten by the owner"`
	code, body, head := quoted(t, router, http.MethodPatch, "/api/v1/billing/plans/"+key,
		`{"name":"rewritten by the owner","cents":4000}`, "")
	if code != http.StatusOK || !strings.Contains(body, rewritten) {
		t.Fatalf("the row's own tenant PATCH, quoting nothing = %d %s, want 200 with the row it wrote in it", code, body)
	}
	if tag := head.Get("ETag"); tag != "" {
		t.Errorf("the accepted write of a row whose table keeps no write count answered ETag %q, want no tag at all: a value this resource will refuse to be quoted back is not a tag", tag)
	}
	if after := readPlan(t, admin, key); after.name != "rewritten by the owner" {
		t.Fatalf("the answer above was not the accepted write it says it was: the table holds %+v", after)
	}

	port := &recordedPort{Translations: &stubPort{}}
	articleRouter, articleID := mountedArticle(t, port)
	code, body, head = quoted(t, articleRouter, http.MethodPatch, "/api/v1/articles/article/"+articleID+"?lang=pt-PT", `{"title":"Sobre nós."}`, "")
	if code != http.StatusOK {
		t.Fatalf("the locale write of an uncounted row = %d %s, want 200", code, body)
	}
	if tag := head.Get("ETag"); tag != "" {
		t.Errorf("the accepted locale write of a row whose table keeps no write count answered ETag %q, want no tag at all", tag)
	}
	if len(port.saved) != 1 || port.saved[0].Locale != "pt-PT" {
		t.Fatalf("the locale write answered 200 without filing the translation: %+v", port.saved)
	}
}
