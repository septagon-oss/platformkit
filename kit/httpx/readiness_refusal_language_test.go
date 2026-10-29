package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/health"
)

func TestReadinessRefusalUsesPublicCopyInTheRequestedLanguage(t *testing.T) {
	api, router := setupStatusFault(t, faultCase{})
	const checkName = "private-database-primary"
	health.Register(api, []health.Check{broken{checkName}})
	for _, language := range []string{"en", "pt-PT"} {
		t.Run(language, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://pod.invalid/ready", nil)
			req.Header.Set("Accept", "text/html")
			req.Header.Set("Accept-Language", language)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != http.StatusServiceUnavailable || !strings.HasPrefix(res.Header().Get("Content-Type"), "text/html") {
				t.Fatalf("unready probe answered %d %s, want a 503 page", res.Code, res.Header().Get("Content-Type"))
			}
			if got := res.Header().Get("Content-Language"); got != language {
				t.Errorf("readiness refusal language = %q, want %q", got, language)
			}
			if strings.Contains(res.Body.String(), checkName) {
				t.Errorf("readiness page exposes internal check %q instead of the catalogued public outage sentence", checkName)
			}
		})
	}
}
