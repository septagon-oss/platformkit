package httpx_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAReplayRechecksGrantsAndSeparatesCallers(t *testing.T) {
	_, conn := dbtest.Schema(t)
	f := &fixture{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme"}, allow: true}
	f.signedIn()
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), Conn: conn, Tenants: f, Authorize: f,
		Authenticate: f.authenticate, Idempotency: idempotency.Memory(nil),
		Log: slog.New(slog.NewTextHandler(&lines{}, nil)),
	})
	runs := 0
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "create-authorized-record", Method: http.MethodPost, Path: "/records",
		DefaultStatus: http.StatusCreated,
	}, httpx.Permission("record:write"), func(context.Context, *struct{}) (*struct{}, error) {
		runs++
		return &struct{}{}, nil
	})
	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://"+host+at(api, "/records"), strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set(httpx.IdempotencyKeyHeader, "same-key")
		req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	for range 2 {
		if got := send(); got.Code != http.StatusCreated {
			t.Fatalf("allowed write/replay = %d %s", got.Code, got.Body.String())
		}
	}
	f.allow = false
	if got := send(); got.Code != http.StatusForbidden {
		t.Errorf("revoked grant replay = %d %s, want 403", got.Code, got.Body.String())
	}
	if runs != 1 {
		t.Fatalf("handler ran %d times before caller change, want 1", runs)
	}
	f.allow = true
	f.signedIn()
	if got := send(); got.Code != http.StatusCreated {
		t.Fatalf("second caller = %d %s", got.Code, got.Body.String())
	}
	if runs != 2 {
		t.Errorf("distinct caller did not get its own write: runs=%d", runs)
	}
}
