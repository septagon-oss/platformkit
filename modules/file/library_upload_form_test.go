package file_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// The library's upload control is a form a person submits from the page. Whatever
// the door does with the bytes, the person submitting it must land on a page — the
// library again, or a refusal drawn as a page — and never on the JSON row the API
// answers a program with.
func TestTheLibrarysUploadFormLandsThePersonOnAPage(t *testing.T) {
	router, admin, _, _, _ := libraryFixture(t)
	page := libraryRequest(t, router, http.MethodGet, libraryAddress, host, "", true)
	action := regexp.MustCompile(`<form[^>]*action="([^"]+)"[^>]*enctype="multipart/form-data"`).FindStringSubmatch(page.Body.String())
	if len(action) != 2 {
		action = regexp.MustCompile(`<form[^>]*enctype="multipart/form-data"[^>]*action="([^"]+)"`).FindStringSubmatch(page.Body.String())
	}
	if len(action) != 2 {
		t.Fatal("the library draws no upload form")
	}
	at := strings.ReplaceAll(action[1], "&amp;", "&")
	var form bytes.Buffer
	w := multipart.NewWriter(&form)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="wide.jpg"`)
	header.Set("Content-Type", "image/jpeg")
	part, err := w.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(wideFrame(t)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://"+host+at, &form)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Origin", "http://"+host)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var stored int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM files").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	landed := rec.Code == http.StatusSeeOther || strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html")
	if !landed || stored != 1 {
		t.Errorf("submitting the library's upload form (POST %s) = %d %s, %q; stored files=%d",
			at, rec.Code, rec.Header().Get("Content-Type"), firstLine(rec.Body.String()), stored)
	}
}

func firstLine(s string) string {
	if len(s) > 160 {
		return s[:160]
	}
	return s
}
