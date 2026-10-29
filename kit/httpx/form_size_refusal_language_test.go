package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestOversizedFormRefusalSpeaksTheTenantsLanguage(t *testing.T) {
	api, router := setupStatusFault(t, faultCase{configure: func(f *fixture, _ *httpx.Options) {
		f.tenant.Languages = &tenancy.Languages{Default: "pt-PT", Others: []string{"en"}}
	}})
	public := api.Surfaces(probe).Public
	httpx.HTML(public, huma.Operation{OperationID: "bounded-localized-form", Method: http.MethodPost,
		Path: "/bounded-localized-form", MaxBodyBytes: 8}, httpx.Public(),
		func(context.Context, *struct{ RawBody []byte }) (*httpx.Page, error) {
			t.Error("oversized form reached the handler")
			return &httpx.Page{Status: 200}, nil
		})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+host+public.PagePath("/bounded-localized-form"), strings.NewReader("token=too-long"))
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusRequestEntityTooLarge || !strings.HasPrefix(res.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("oversized form answered %d %s, want a 413 page", res.Code, res.Header().Get("Content-Type"))
	}
	if got := res.Header().Get("Content-Language"); got != "pt-PT" {
		t.Errorf("oversized form refusal language = %q, want pt-PT", got)
	}
	if strings.Contains(res.Body.String(), "request body is too large") {
		t.Error("the Portuguese reader receives the framework's English refusal")
	}
}
