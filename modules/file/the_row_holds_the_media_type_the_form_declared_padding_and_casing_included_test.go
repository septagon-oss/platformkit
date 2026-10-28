package file_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestTheRowHoldsTheMediaTypeTheFormDeclaredPaddingAndCasingIncluded follows one
// declared media type from the form bytes to the content_type column.
//
// The library's two readings of that column — essenceSQL in SQL, contracts.Essence
// in Go — can only agree over the values the column is able to hold, and the value
// it holds is what this module's own route files: the handler's read of the part's
// declared type, through the upload command, into the row. This asks the route and
// reads the table, so a step between them that trimmed, lower-cased or otherwise
// rewrote the declaration refuses here rather than moving which forms the two
// readings are asked about. The padding inside a value and the casing of it arrive
// as they were declared; the whitespace at the ends of the whole header value does
// not, because the header reader removes it before this module sees it.
func TestTheRowHoldsTheMediaTypeTheFormDeclaredPaddingAndCasingIncluded(t *testing.T) {
	router, admin, _ := mountedOn(t, map[string]tenancy.Tenant{host: acme})
	declared := []struct{ says, holds string }{
		{says: "text/plain", holds: "text/plain"},
		{says: "Text/Plain", holds: "Text/Plain"},
		{says: "text/plain;charset=utf-8", holds: "text/plain;charset=utf-8"},
		{says: "text/plain\t;charset=utf-8", holds: "text/plain\t;charset=utf-8"},
		{says: "text/plain ; charset=utf-8", holds: "text/plain ; charset=utf-8"},
		{says: " text/plain ", holds: "text/plain"},
	}
	for i, one := range declared {
		name := "declared-" + string(rune('a'+i)) + ".txt"
		code, out := declare(t, router, name, one.says, "a line of text\r\n")
		if code != http.StatusCreated {
			t.Fatalf("an upload declaring %q answered %d: %s", one.says, code, out)
		}
		var held string
		if err := admin.QueryRowContext(t.Context(),
			`SELECT content_type FROM files WHERE name = $1`, name).Scan(&held); err != nil {
			t.Fatalf("reading the row filed by a form declaring %q: %v", one.says, err)
		}
		if held != one.holds {
			t.Errorf("a form declaring %q is filed as %q: the column holds what the route is handed", one.says, held)
		}
	}
}

// declaredBoundary separates the parts this case writes by hand.
const declaredBoundary = "pkit-row-boundary"

// declare posts one hand-written multipart body, byte for byte the way a client
// writes it, so the declared type reaches the route without passing a writer that
// could quote or re-arrange it on the way.
func declare(t *testing.T, router http.Handler, name, declared, body string) (int, string) {
	t.Helper()
	form := "--" + declaredBoundary + "\r\n" +
		"Content-Disposition: form-data; name=\"file\"; filename=\"" + name + "\"\r\n" +
		"Content-Type: " + declared + "\r\n\r\n" +
		body + "--" + declaredBoundary + "--\r\n"
	req := httptest.NewRequest(http.MethodPost, "http://"+host+files, strings.NewReader(form))
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+declaredBoundary)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}
