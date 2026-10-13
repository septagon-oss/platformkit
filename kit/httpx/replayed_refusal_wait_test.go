package httpx_test

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestReplayedRefusalWaitIgnoresWindowsWithQuotaRemaining(t *testing.T) {
	_, conn := dbtest.Schema(t)
	f := &fixture{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme"}, app: conn, logs: &lines{}}
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: f, Conn: conn,
		Authorize: f, Authenticate: f.authenticate,
		WriteLimiter: limit.Postgres(func(context.Context) (*db.Conn, bool) { return conn, true }),
		Idempotency:  idempotency.Postgres(httpx.ConnFrom), Log: slog.New(slog.DiscardHandler),
	})
	counter := limit.Postgres(httpx.ConnFrom)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), f.tenant), conn)
	if allowed, _, _, err := counter.Reserve(ctx, "short-capped-entries", 1, 2*time.Second); err != nil || !allowed {
		t.Fatalf("seed counter: allowed=%v error=%v", allowed, err)
	}
	runs := 0
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "create-short-capped-entry", Method: http.MethodPost, Path: "/short-capped-entries",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		runs++
		allowed, remaining, left, err := counter.Reserve(ctx, "short-capped-entries", 1, 2*time.Second)
		if err != nil {
			return nil, err
		}
		httpx.SayCounted(ctx, httpx.Counted{
			Name: "short-capped-entries", Quota: 1, Window: 2 * time.Second,
			Remaining: remaining, Left: left, Refused: !allowed,
		})
		return nil, problem.New(http.StatusTooManyRequests, "entry limit exhausted")
	})
	const path = "/api/v1/public/probe/short-capped-entries"
	first := keyedPost(t, router, http.MethodPost, path, "short-refusal", "{}")
	if first.Code != http.StatusTooManyRequests {
		t.Fatalf("first answer = %d %s; want the deciding refusal", first.Code, first.Body.String())
	}
	firstWait, err := strconv.Atoi(first.Header().Get(httpx.RetryAfterHeader))
	if err != nil || firstWait < 1 || firstWait > 2 {
		t.Fatalf("deciding Retry-After = %q; want the short counter's wait", first.Header().Get(httpx.RetryAfterHeader))
	}
	if !strings.Contains(first.Header().Get(httpx.RateLimitHeader), `"public-writes";r=59;`) {
		t.Fatalf("deciding readings = %q; public counter must still allow another attempt", first.Header().Get(httpx.RateLimitHeader))
	}
	t.Logf("deciding Retry-After=%d RateLimit=%s", firstWait, first.Header().Get(httpx.RateLimitHeader))
	for _, after := range []struct {
		name  string
		pause time.Duration
	}{{"immediate retry", 0}, {"after the refusing window closes", time.Duration(firstWait)*time.Second + 100*time.Millisecond}} {
		t.Run(after.name, func(t *testing.T) {
			time.Sleep(after.pause)
			replay := keyedPost(t, router, http.MethodPost, path, "short-refusal", "{}")
			if replay.Code != http.StatusTooManyRequests || replay.Body.String() != first.Body.String() || runs != 1 {
				t.Fatalf("replay status=%d body=%q executions=%d; want the stored refusal without another execution", replay.Code, replay.Body.String(), runs)
			}
			if got := replay.Header().Get(httpx.RateLimitPolicyHeader); got != first.Header().Get(httpx.RateLimitPolicyHeader) {
				t.Errorf("replayed policy = %q; want the deciding policy", got)
			}
			maximumWait := firstWait
			if after.pause > 0 {
				maximumWait = 1
				if !strings.Contains(replay.Header().Get(httpx.RateLimitHeader), `"short-capped-entries";r=0;t=0`) {
					t.Errorf("replayed readings = %q; the refusing window should have closed", replay.Header().Get(httpx.RateLimitHeader))
				}
			}
			wait, err := strconv.Atoi(replay.Header().Get(httpx.RetryAfterHeader))
			if err != nil || wait < 1 || wait > maximumWait {
				t.Errorf("replayed Retry-After = %q, deciding refusal said %d seconds; want at most %d, ignoring the public window with quota remaining (RateLimit=%s)", replay.Header().Get(httpx.RetryAfterHeader), firstWait, maximumWait, replay.Header().Get(httpx.RateLimitHeader))
			}
		})
	}
}
