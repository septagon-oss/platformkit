package httpx_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/problem"
)

func TestReplayedLimitRefusalKeepsTheDecidingPolicyAndWait(t *testing.T) {
	api, router, _ := keyedSetup(t, idempotency.Postgres(httpx.ConnFrom), nil)
	counter := limit.Memory()
	runs := 0
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "create-limited-entry", Method: http.MethodPost, Path: "/limited-entries",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		runs++
		allowed, remaining, left, err := counter.Reserve(ctx, "entries", 1, time.Hour)
		if err != nil {
			return nil, err
		}
		httpx.SayCounted(ctx, httpx.Counted{
			Name: "entries", Quota: 1, Window: time.Hour,
			Remaining: remaining, Left: left, Refused: !allowed,
		})
		if !allowed {
			return nil, problem.New(http.StatusTooManyRequests, "entry limit exhausted")
		}
		return &struct{}{}, nil
	})
	const path = "/api/v1/public/probe/limited-entries"
	if got := keyedPost(t, router, http.MethodPost, path, "first", "{}"); got.Code != http.StatusNoContent {
		t.Fatalf("first entry = %d %s", got.Code, got.Body.String())
	}
	refused := keyedPost(t, router, http.MethodPost, path, "second", "{}")
	replay := keyedPost(t, router, http.MethodPost, path, "second", "{}")
	if refused.Code != http.StatusTooManyRequests || replay.Code != http.StatusTooManyRequests || runs != 2 {
		t.Fatalf("refusal=%d replay=%d executions=%d; want 429, 429, 2", refused.Code, replay.Code, runs)
	}
	for _, header := range []string{httpx.RateLimitPolicyHeader, httpx.RateLimitHeader} {
		want := refused.Header().Get(header)
		if want == "" {
			t.Fatalf("original refusal did not state %s", header)
		}
		if got := replay.Header().Get(header); got == "" || !strings.Contains(got, `"entries"`) {
			t.Errorf("replayed %s = %q, original deciding counter said %q", header, got, want)
		}
	}
	want, err := strconv.Atoi(refused.Header().Get(httpx.RetryAfterHeader))
	if err != nil {
		t.Fatal(err)
	}
	got, err := strconv.Atoi(replay.Header().Get(httpx.RetryAfterHeader))
	if err != nil || got < want-5 || got > want {
		t.Errorf("replayed Retry-After = %q, original deciding counter said %d seconds", replay.Header().Get(httpx.RetryAfterHeader), want)
	}
}
