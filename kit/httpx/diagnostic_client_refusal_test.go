package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	uipage "github.com/septagon-oss/platformkit/ui/page"
	g "maragu.dev/gomponents"
)

func TestClientRefusalKeepsItsDiagnosticOutOfTheBrowser(t *testing.T) {
	api, router := setupStatusFault(t, faultCase{})
	public := api.Surfaces(probe).Public
	shell := uipage.Shell{Frame: func(_ context.Context, _ uipage.Request, body []g.Node) g.Node {
		return uipage.Bare(body)
	}}
	const diagnostic = "database-primary.internal SELECT secret FROM credentials"
	for _, status := range []int{401, 403, 409, 422, 503} {
		path := "/private-diagnostic-" + strconv.Itoa(status)
		uipage.Serve(public, shell, uipage.Route{
			ID: "private-diagnostic-" + strconv.Itoa(status), Method: http.MethodGet, Path: path,
		}, httpx.Public(), func(context.Context, uipage.Request, *struct{}) (uipage.View, error) {
			refused := problem.New(status, diagnostic)
			refused.Diagnostic = true
			return uipage.View{}, refused
		})
		for _, accept := range []string{"text/html", "application/json"} {
			t.Run(strconv.Itoa(status)+"/"+accept, func(t *testing.T) {
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
					"http://"+host+public.PagePath(path), nil)
				req.Header.Set("Accept", accept)
				res := httptest.NewRecorder()
				router.ServeHTTP(res, req)
				if res.Code != status {
					t.Fatalf("status = %d, want %d", res.Code, status)
				}
				if accept == "text/html" {
					if !strings.HasPrefix(res.Header().Get("Content-Type"), "text/html") {
						t.Fatal("browser did not receive the refusal page")
					}
					if strings.Contains(res.Body.String(), diagnostic) {
						t.Errorf("status %d page exposes the private diagnostic: %s", status, diagnostic)
					}
				} else if !strings.Contains(res.Body.String(), diagnostic) {
					t.Error("the machine-readable refusal lost its diagnostic")
				}
			})
		}
	}
}
