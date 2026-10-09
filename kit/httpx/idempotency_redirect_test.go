package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
)

func TestAKeyedRedirectKeepsItsDestinationOnReplay(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		name, header, status := "browser", "Location", http.StatusSeeOther
		if htmx {
			name, header, status = "htmx", "HX-Redirect", http.StatusNoContent
		}
		t.Run(name, func(t *testing.T) {
			api, router, _ := keyedSetup(t, idempotency.Memory(nil), nil)
			runs := 0
			httpx.HTML(api.Surfaces(probe).App, huma.Operation{
				OperationID: "create-redirected-write", Method: http.MethodPost, Path: "/redirected-writes",
			}, httpx.Public(), func(context.Context, *struct{}) (*httpx.Page, error) {
				runs++
				return nil, httpx.SeeOther("/app/probe/finished")
			})
			send := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, "http://"+host+"/app/probe/redirected-writes", strings.NewReader("{}"))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set(httpx.IdempotencyKeyHeader, "redirect-once")
				if htmx {
					r.Header.Set("HX-Request", "true")
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				return w
			}
			for attempt := range 2 {
				got := send()
				if got.Code != status || got.Header().Get(header) != "/app/probe/finished" {
					t.Errorf("attempt %d: status=%d %s=%q; want %d and /app/probe/finished", attempt+1, got.Code, header, got.Header().Get(header), status)
				}
			}
			if runs != 1 {
				t.Errorf("handler executions=%d, want 1", runs)
			}
		})
	}
}
