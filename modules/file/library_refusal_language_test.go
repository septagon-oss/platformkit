package file_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestStaleCaptionRefusalSpeaksTheRequestedLanguage(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	id, _ := uploadFrame(t, router, files)
	panel := libraryRequest(t, router, http.MethodGet, libraryAddress+"?file="+id, host, "", true)
	stamp := regexp.MustCompile(`name="updated" value="([^"]+)"`).FindStringSubmatch(panel.Body.String())
	if len(stamp) != 2 {
		t.Fatal("the panel has no revision stamp")
	}
	post := func(lang, alt string) *httptest.ResponseRecorder {
		body := url.Values{"updated": {stamp[1]}, "alt": {alt}}.Encode()
		req := httptest.NewRequest(http.MethodPost, "http://"+host+libraryAddress+"/"+id+"/alt", strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept-Language", lang)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := post("en", "first caption"); rec.Code != http.StatusSeeOther {
		t.Fatalf("caption = %d %s", rec.Code, rec.Body.String())
	}
	for _, check := range []struct{ language, words string }{{"en", "this file changed"}, {"pt-PT", "Este ficheiro mudou"}} {
		rec := post(check.language, "stale caption")
		if rec.Code != http.StatusConflict {
			t.Fatalf("stale caption = %d", rec.Code)
		}
		if rec.Header().Get("Content-Language") != check.language || !strings.Contains(rec.Body.String(), check.words) {
			t.Errorf("%s refusal is missing the request's language or translated reason", check.language)
		}
	}
}
