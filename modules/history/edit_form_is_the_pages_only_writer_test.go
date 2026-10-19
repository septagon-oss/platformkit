package history_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// TestAVersionedPagesEditFormIsTheOnlyWriterOfItsBody checks the content page a
// person actually edits, under the version store that counts its writes. The form
// quotes the row's revision and keys an unsaved draft by the record and the field;
// it draws no address the browser could PATCH the body to while the person types,
// because a second writer would move the row off the revision the form quotes.
// The Save itself then writes exactly one version, and the same form posted
// again, still quoting the revision it was read at, is refused and writes none;
// the form answers that refusal by drawing itself again, as any refused form does.
func TestAVersionedPagesEditFormIsTheOnlyWriterOfItsBody(t *testing.T) {
	w, router, created, screen := contentHistoryFixture(t)
	address := screen + "/" + created.ID.String()

	code, edit := call(t, router, acmeHost, http.MethodGet, address+"/edit", "")
	if code != http.StatusOK {
		t.Fatalf("edit page = %d %s", code, edit)
	}
	if !strings.Contains(edit, `data-richtext-draft="`) || !strings.Contains(edit, created.ID.String()+`/body"`) {
		t.Errorf("the edit form keys no unsaved draft by the record and its body")
	}
	for _, second := range []string{"data-richtext-endpoint=", "data-richtext-etag="} {
		if strings.Contains(edit, second) {
			t.Errorf("the edit form draws %s, a second writer of the body beside its own Save", second)
		}
	}
	if !strings.Contains(edit, `name="expectedRevision" type="hidden"`) || !strings.Contains(edit, `value="2"`) {
		t.Fatalf("the edit form quotes no revision 2 for the write it posts")
	}

	before := len(w.versionsOf(created.ID))
	form := url.Values{"slug": {"language"}, "title": {"First draft"}, "body": {"Saved text"}, "expectedRevision": {"2"}}
	if rec := postForm(router, address, form); rec.Code >= http.StatusBadRequest {
		t.Fatalf("saving the edit form = %d %s", rec.Code, rec.Body.String())
	}
	var body string
	var revision int64
	if err := w.admin.QueryRowContext(t.Context(),
		"SELECT body, revision FROM contents WHERE id = $1", created.ID).Scan(&body, &revision); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(body) != "Saved text" || revision != 3 {
		t.Errorf("the Save left body %q at revision %d, want %q at 3", body, revision, "Saved text")
	}
	if after := len(w.versionsOf(created.ID)); after != before+1 {
		t.Errorf("the Save left %d versions after %d, want exactly one more", after, before)
	}

	form.Set("body", "Stale text")
	if rec := postForm(router, address, form); rec.Code < http.StatusBadRequest {
		t.Errorf("the same form posted again at revision 2 = %d, want a refusal", rec.Code)
	}
	if err := w.admin.QueryRowContext(t.Context(),
		"SELECT body, revision FROM contents WHERE id = $1", created.ID).Scan(&body, &revision); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(body) != "Saved text" || revision != 3 {
		t.Errorf("the refused post left body %q at revision %d, want %q at 3", body, revision, "Saved text")
	}
	if after := len(w.versionsOf(created.ID)); after != before+1 {
		t.Errorf("the refused post left %d versions, want %d", after, before+1)
	}
}

func postForm(router http.Handler, address string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "http://"+acmeHost+address, strings.NewReader(form.Encode()))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
