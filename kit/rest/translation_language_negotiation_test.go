package rest_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestTranslationReadsNegotiateCookieAndAcceptLanguage(t *testing.T) {
	router, articleID := mountedArticle(t, articlePort)
	for _, preference := range []string{"cookie", "header"} {
		t.Run(preference, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://"+host+"/api/v1/articles/article/"+articleID, nil)
			req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
			if preference == "cookie" {
				req.AddCookie(&http.Cookie{Name: "lang", Value: "pt-PT"})
			} else {
				req.Header.Set("Accept-Language", "pt-PT, en;q=0.5")
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("read = %d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Content-Language") != "pt-PT" || !strings.Contains(w.Body.String(), `"title":"Sobre nós."`) {
				t.Errorf("%s preference ignored: Content-Language %q, body %s", preference, w.Header().Get("Content-Language"), w.Body.String())
			}
		})
	}
}
