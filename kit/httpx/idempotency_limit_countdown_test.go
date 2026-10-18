package httpx_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/problem"
)

func TestStoredLimitRefusalCountsDownBothWindowsWithoutSpendingAgain(t *testing.T) {
	api, router, _ := keyedSetup(t, idempotency.Postgres(httpx.ConnFrom), nil)
	counter := limit.Memory()
	runs := 0
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "create-counted-entry", Method: http.MethodPost, Path: "/counted-entries",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		runs++
		for _, policy := range []struct {
			name   string
			window time.Duration
		}{{"short-entries", 2 * time.Second}, {"long-entries", 4 * time.Second}} {
			allowed, remaining, left, err := counter.Reserve(ctx, policy.name, 0, policy.window)
			if err != nil {
				return nil, err
			}
			httpx.SayCounted(ctx, httpx.Counted{
				Name: policy.name, Quota: 0, Window: policy.window,
				Remaining: remaining, Left: left, Refused: !allowed,
			})
		}
		return nil, problem.New(http.StatusTooManyRequests, "entry limit exhausted")
	})
	const path = "/api/v1/public/probe/counted-entries"
	first := keyedPost(t, router, http.MethodPost, path, "countdown", "{}")
	if first.Code != http.StatusTooManyRequests {
		t.Fatalf("first answer = %d %s", first.Code, first.Body.String())
	}
	wantPolicy := `"short-entries";q=0;w=2,"long-entries";q=0;w=4`
	if got := first.Header().Get(httpx.RateLimitPolicyHeader); got != wantPolicy {
		t.Fatalf("original policy = %q, want %q", got, wantPolicy)
	}
	firstWait, err := strconv.Atoi(first.Header().Get(httpx.RetryAfterHeader))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	replay := keyedPost(t, router, http.MethodPost, path, "countdown", "{}")
	if replay.Code != http.StatusTooManyRequests || replay.Body.String() != first.Body.String() || runs != 1 {
		t.Fatalf("replay status=%d executions=%d body=%q", replay.Code, runs, replay.Body.String())
	}
	if got := replay.Header().Get(httpx.RateLimitPolicyHeader); got != wantPolicy {
		t.Errorf("replayed policy = %q, want %q", got, wantPolicy)
	}
	wait, err := strconv.Atoi(replay.Header().Get(httpx.RetryAfterHeader))
	if err != nil || wait < 1 || wait > max(firstWait-1, 1) {
		t.Errorf("replayed wait = %q, original wait = %d; want elapsed time deducted", replay.Header().Get(httpx.RetryAfterHeader), firstWait)
	}
	time.Sleep(4 * time.Second)
	expired := keyedPost(t, router, http.MethodPost, path, "countdown", "{}")
	if expired.Code != http.StatusTooManyRequests || expired.Body.String() != first.Body.String() || runs != 1 {
		t.Fatalf("expired replay status=%d executions=%d body=%q", expired.Code, runs, expired.Body.String())
	}
	if got := expired.Header().Get(httpx.RateLimitPolicyHeader); got != wantPolicy {
		t.Errorf("expired policy = %q, want %q", got, wantPolicy)
	}
	if got := expired.Header().Get(httpx.RateLimitHeader); got != `"short-entries";r=0;t=0,"long-entries";r=0;t=0` {
		t.Errorf("expired readings = %q; want both windows closed", got)
	}
	if got := expired.Header().Get(httpx.RetryAfterHeader); got != "1" {
		t.Errorf("expired wait = %q; want the documented one-second floor", got)
	}
}
