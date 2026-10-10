package httpx_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAPublicHeadDoesNotSpendTheWriteLimit(t *testing.T) {
	_, conn := dbtest.Schema(t)
	limiter := &refusingSpentCount{}
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), Conn: conn,
		PublicHost: host, Installation: installationHost,
		Tenants: loaderFunc(func(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
			return tenant, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return false, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		WriteLimiter: limiter,
		Log:          slog.New(slog.DiscardHandler),
	})
	reached := 0
	surface := api.Surfaces("reading").Public
	httpx.Register(surface, huma.Operation{
		OperationID: "reading-head", Method: http.MethodHead, Path: "/face",
	}, httpx.Public(), func(context.Context, *struct{}) (*body, error) {
		reached++
		return &body{}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodHead, "http://"+host+surface.Path("/face"), nil))
	if res.Code != http.StatusOK || reached != 1 {
		t.Fatalf("public HEAD: status %d, handler calls %d; want 200 and 1", res.Code, reached)
	}
	if got := limiter.consulted.Load(); got != 0 {
		t.Errorf("public HEAD consulted the write counter %d times, want 0", got)
	}
	if cookies := res.Result().Cookies(); len(cookies) != 0 {
		t.Errorf("public HEAD set cookies: %v", cookies)
	}
}
