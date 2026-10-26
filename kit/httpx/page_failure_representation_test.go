package httpx_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	uipage "github.com/septagon-oss/platformkit/ui/page"
	g "maragu.dev/gomponents"
)

func TestPageFailuresUseTheNegotiatedErrorDocument(t *testing.T) {
	api, router := setupStatusFault(t, faultCase{})
	public := api.Surfaces(probe).Public
	shell := uipage.Shell{Frame: func(_ context.Context, _ uipage.Request, body []g.Node) g.Node {
		return uipage.Bare(body)
	}}
	for _, status := range []int{500, 503} {
		name := http.StatusText(status)
		path := "/" + strings.ReplaceAll(strings.ToLower(name), " ", "-")
		uipage.Serve(public, shell, uipage.Route{
			ID: "page-" + strings.TrimPrefix(path, "/"), Method: http.MethodGet, Path: path,
		}, httpx.Public(), func(context.Context, uipage.Request, *struct{}) (uipage.View, error) {
			if status == 500 {
				return uipage.View{}, errors.New("private database connection failed")
			}
			return uipage.View{}, problem.New(status, "service temporarily unavailable")
		})
		t.Run(name, func(t *testing.T) {
			for _, accept := range []string{"text/html", "application/json"} {
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
					"http://"+host+public.PagePath(path), nil)
				req.Header.Set("Accept", accept)
				res := httptest.NewRecorder()
				router.ServeHTTP(res, req)
				if res.Code != status {
					t.Fatalf("%s: status = %d, want %d", accept, res.Code, status)
				}
				want := "application/problem+json"
				if accept == "text/html" {
					want = "text/html"
					if !strings.Contains(res.Body.String(), "<html") {
						t.Error("browser refusal has no HTML document")
					}
				}
				if !strings.HasPrefix(res.Header().Get("Content-Type"), want) {
					t.Errorf("%s: Content-Type = %q, want %s; body = %s", accept, res.Header().Get("Content-Type"), want, res.Body.String())
				}
			}
		})
	}

	httpx.HTML(public, huma.Operation{OperationID: "bounded-form", Method: http.MethodPost,
		Path: "/bounded-form", MaxBodyBytes: 8}, httpx.Public(),
		func(context.Context, *struct{ RawBody []byte }) (*httpx.Page, error) {
			t.Error("oversized form reached its handler")
			return &httpx.Page{Status: 200}, nil
		})
	t.Run("oversized form", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
			"http://"+host+public.PagePath("/bounded-form"), strings.NewReader("token=too-long"))
		req.Header.Set("Accept", "text/html")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized form status = %d, want 413: %s", res.Code, res.Body.String())
		}
		if !strings.HasPrefix(res.Header().Get("Content-Type"), "text/html") {
			t.Errorf("oversized page form received %s: %s; want HTML", res.Header().Get("Content-Type"), res.Body.String())
		}
		if !strings.Contains(res.Body.String(), "<html") {
			t.Error("oversized page form refusal has no HTML document")
		}
	})
}
