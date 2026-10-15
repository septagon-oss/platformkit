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
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAnAskRefusalPreservesTheWholePolicyFieldOnRetry(t *testing.T) {
	_, conn := dbtest.Schema(t)
	reach := &reachF{recipients: []uuid.UUID{uuid.New()}}
	f := &fixture{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme"}, app: conn, logs: &lines{}}
	f.signedIn()
	f.allow = true
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), Installation: host, PublicHost: host,
		Tenants: f, Conn: conn, Authorize: f, Authenticate: f.authenticate,
		Access: reach, WriteLimiter: limit.Memory(),
		Idempotency: idempotency.Postgres(httpx.ConnFrom), Log: slog.New(slog.DiscardHandler),
	})
	api.Declare([]tenancy.Grant{{Permission: "widget:read", Label: "read widgets"}})
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "ask-widget-policy", Method: http.MethodPost, Path: "/asks",
		DefaultStatus: http.StatusAccepted,
	}, httpx.SignedIn(), func(ctx context.Context, in *askInput) (*struct{}, error) {
		return nil, httpx.Ask(ctx, httpx.AccessAsk{Permission: in.Body.Permission, Path: in.Body.Path})
	})
	ask := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://"+host+at(api, "/asks"),
			strings.NewReader(`{"permission":"widget:read","path":"/app/widget/widgets"}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
		if key != "" {
			req.Header.Set(httpx.IdempotencyKeyHeader, key)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	for range 3 {
		if got := ask(""); got.Code != http.StatusAccepted {
			t.Fatalf("ask within quota = %d %s", got.Code, got.Body.String())
		}
	}
	first := ask("policy-refusal")
	if first.Code != http.StatusTooManyRequests {
		t.Fatalf("ask beyond quota = %d %s", first.Code, first.Body.String())
	}
	policy := first.Header().Get(httpx.RateLimitPolicyHeader)
	if policy == "" {
		t.Fatal("deciding refusal named no policy")
	}
	replay := ask("policy-refusal")
	if replay.Code != http.StatusTooManyRequests || replay.Body.String() != first.Body.String() || len(reach.told) != 3 {
		t.Fatalf("replay=%d body=%q notices=%d; want same refusal without another notice", replay.Code, replay.Body.String(), len(reach.told))
	}
	if got := replay.Header().Get(httpx.RateLimitPolicyHeader); got != policy {
		t.Errorf("replayed RateLimit-Policy = %q; original deciding refusal named %q", got, policy)
	}
}
