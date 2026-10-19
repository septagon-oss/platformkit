package history_test

import (
	"net/http"
	"testing"
)

func TestRestoringAnEmptyBodyClearsTheLaterBody(t *testing.T) {
	w := newWorld(t)
	router := w.mount(grants{memoRead: true, memoWrite: true})
	id := create(t, router, acmeHost, "Empty draft")
	if code, body := call(t, router, acmeHost, http.MethodPatch, address(id),
		`{"body":"Text added after the first version"}`); code != http.StatusOK {
		t.Fatalf("write the second version = %d %s", code, body)
	}
	before := w.versionsOf(id)
	if len(before) != 2 {
		t.Fatalf("two writes kept %d versions", len(before))
	}
	if code, body := restore(t, router, acmeHost, id, 1, 2); code != http.StatusOK {
		t.Fatalf("restore the empty draft = %d %s", code, body)
	}
	var body string
	if err := w.admin.QueryRowContext(t.Context(),
		"SELECT body FROM history_memos WHERE id = $1", id).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != "" {
		t.Errorf("restoring version 1 kept the later body %q; want the original empty body", body)
	}
	after := w.versionsOf(id)
	if len(after) != 3 {
		t.Fatalf("restore kept %d versions, want 3", len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("restore rewrote version %d", i+1)
		}
	}
}
