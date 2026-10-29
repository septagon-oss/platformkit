package rest_test

// A row nobody counts writes on sends no tag, and the door that reads it is the
// door whose silence that is.
//
// The refusal a quoted tag earns against a resource that keeps no write count —
// 422 and the sentence saying the resource keeps none — is declared in patch.go
// and request.go, and every case that reaches it quotes a tag nobody could have
// read. That leaves the premise unchecked at the door that composes the answer
// from the row itself: the plain read. The translated read of such a row is asked
// for its tag and answers none, but that read is mounted through the translation
// branch, while the plain read is a different closure composing its tag from the
// same reading of the row, and nothing asks what it sends for an entity with no
// counter. The harm of a read that started answering a tag here is not a leak: it
// is a person handed a value the same resource will refuse to accept back, with
// every counted row's read still answering its own count beside it. So this case
// asks the plain read, reaching the assertion through the row's own key and a
// status rather than through anything a refusal prints.

import (
	"net/http"
	"strings"
	"testing"
)

func TestThePlainReadOfARowWhoseTableKeepsNoWriteCountAnswersNoTag(t *testing.T) {
	_, router, _ := sharedCatalogue(t, true)
	code, body := call(t, router, http.MethodPost, "/api/v1/billing/plans", `{"name":"Team","cents":4000}`)
	if code != http.StatusCreated {
		t.Fatalf("POST the catalogue row = %d %s, want 201", code, body)
	}
	key := id(t, body)

	code, body, head := quoted(t, router, http.MethodGet, "/api/v1/billing/plans/"+key, "", "")
	if code != http.StatusOK {
		t.Fatalf("the plain read of the uncounted row = %d %s, want 200", code, body)
	}
	if !strings.Contains(body, `"name":"Team"`) {
		t.Fatalf("the plain read answered %s, want the row it read in it", body)
	}
	if tag := head.Get("ETag"); tag != "" {
		t.Errorf("the plain read of a row whose table keeps no write count answered ETag %q, want no tag at all: a value this resource will refuse to be quoted back is not a tag", tag)
	}
}
