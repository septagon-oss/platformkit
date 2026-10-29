package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	uipage "github.com/septagon-oss/platformkit/ui/page"
	g "maragu.dev/gomponents"
)

func TestPageFailureKeepsItsDiagnosticOutOfTheBrowser(t *testing.T) {
	api, router := setupStatusFault(t, faultCase{})
	public := api.Surfaces(probe).Public
	const diagnostic = "database-primary.internal SELECT secret FROM credentials"
	shell := uipage.Shell{Frame: func(_ context.Context, _ uipage.Request, body []g.Node) g.Node {
		return uipage.Bare(body)
	}}
	uipage.Serve(public, shell, uipage.Route{
		ID: "diagnostic-failure", Method: http.MethodGet, Path: "/diagnostic-failure",
	}, httpx.Public(), func(context.Context, uipage.Request, *struct{}) (uipage.View, error) {
		refused := problem.New(http.StatusServiceUnavailable, diagnostic)
		refused.Diagnostic = true
		return uipage.View{}, refused
	})
	for _, accept := range []string{"text/html", "application/json"} {
		t.Run(accept, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
				"http://"+host+public.PagePath("/diagnostic-failure"), nil)
			req.Header.Set("Accept", accept)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", res.Code)
			}
			if accept == "text/html" {
				if !strings.HasPrefix(res.Header().Get("Content-Type"), "text/html") {
					t.Fatal("the browser did not receive its error page")
				}
				if strings.Contains(res.Body.String(), diagnostic) {
					t.Error("the browser page exposes the problem's private diagnostic")
				}
			} else if !strings.Contains(res.Body.String(), diagnostic) {
				t.Error("the machine-readable problem lost its diagnostic")
			}
		})
	}
}
