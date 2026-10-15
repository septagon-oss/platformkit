package file_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestLibraryReadOnlyCredentialCannotWriteEitherSurface(t *testing.T) {
	router, admin, _, _, _ := libraryFixture(t)
	id, _ := uploadFrame(t, router, files)
	var before int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, at := range []string{libraryAddress, "/api/v1/file/quota", files} {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+at, nil)
		req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "read-only"})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("read-only GET %s = %d", at, rec.Code)
		}
	}
	for _, command := range []struct{ method, at, body string }{
		{http.MethodPost, libraryAddress + "/" + id + "/alt", "alt=forbidden"},
		{http.MethodPost, libraryAddress + "/" + id + "/delete", ""},
		{http.MethodPut, files + "/" + id + "/alt", `{"alt":"forbidden"}`},
		{http.MethodDelete, files + "/" + id, ""},
	} {
		req := httptest.NewRequest(command.method, "http://"+host+command.at, strings.NewReader(command.body))
		req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "read-only"})
		if command.method == http.MethodPost {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("read-only %s %s = %d; want 403", command.method, command.at, rec.Code)
		}
	}
	var after, filesLeft int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM files WHERE id=$1 AND alt=''", id).Scan(&filesLeft); err != nil {
		t.Fatal(err)
	}
	if after != before || filesLeft != 1 {
		t.Errorf("route refusals changed data: events %d->%d, unchanged file rows=%d", before, after, filesLeft)
	}
	anonymous := libraryRequest(t, router, http.MethodGet, libraryAddress, host, "", false)
	location, err := url.Parse(anonymous.Header().Get("Location"))
	if err != nil || anonymous.Code != http.StatusSeeOther || location.Path != "/app/admin/login" || location.Query().Get("next") != libraryAddress {
		t.Errorf("anonymous library = %d %s; want sign-in redirect", anonymous.Code, anonymous.Header().Get("Location"))
	}
}
