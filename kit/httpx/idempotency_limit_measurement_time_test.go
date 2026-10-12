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
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestReplayedLimitWaitEndsWhenTheDecidingCounterWindowEnds(t *testing.T) {
	api, router, _ := keyedSetup(t, idempotency.Postgres(httpx.ConnFrom), nil)
	counter := limit.Memory()
	var tenant tenancy.Tenant
	runs := 0
	const window = 5 * time.Second
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "create-delayed-entry", Method: http.MethodPost, Path: "/delayed-entries",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		runs++
		tenant, _ = tenancy.FromContext(ctx)
		allowed, remaining, left, err := counter.Reserve(ctx, "delayed-entries", 0, window)
		if err != nil {
			return nil, err
		}
		httpx.SayCounted(ctx, httpx.Counted{
			Name: "delayed-entries", Quota: 0, Window: window,
			Remaining: remaining, Left: left, Refused: !allowed,
		})
		// Work after the decision must not extend the counter's window on a replay.
		time.Sleep(2100 * time.Millisecond)
		return nil, problem.New(http.StatusTooManyRequests, "entry limit exhausted")
	})
	const path = "/api/v1/public/probe/delayed-entries"
	first := keyedPost(t, router, http.MethodPost, path, "delayed-refusal", "{}")
	if first.Code != http.StatusTooManyRequests {
		t.Fatalf("first answer = %d %s", first.Code, first.Body.String())
	}
	_, left, err := counter.Count(tenancy.WithTenant(t.Context(), tenant), "delayed-entries", window)
	if err != nil {
		t.Fatal(err)
	}
	maximumWait := max(int((left+time.Second-1)/time.Second), 1)
	replay := keyedPost(t, router, http.MethodPost, path, "delayed-refusal", "{}")
	if replay.Code != http.StatusTooManyRequests || runs != 1 || replay.Body.String() != first.Body.String() {
		t.Fatalf("replay status=%d executions=%d body=%q", replay.Code, runs, replay.Body.String())
	}
	if got := replay.Header().Get(httpx.RateLimitPolicyHeader); got != `"delayed-entries";q=0;w=5` {
		t.Errorf("replayed policy = %q; want the deciding policy", got)
	}
	wait, err := strconv.Atoi(replay.Header().Get(httpx.RetryAfterHeader))
	if err != nil || wait < 1 || wait > maximumWait {
		t.Errorf("replayed Retry-After = %q, deciding counter has %s left (at most %d seconds); handler time must not extend its window", replay.Header().Get(httpx.RetryAfterHeader), left, maximumWait)
	}
	reading := replay.Header().Get(httpx.RateLimitHeader)
	seconds, found := strings.CutPrefix(reading, `"delayed-entries";r=0;t=`)
	reset, err := strconv.Atoi(seconds)
	if !found || err != nil || reset < 0 || reset > maximumWait {
		t.Errorf("replayed RateLimit = %q, deciding counter has %s left; handler time must not extend its window", reading, left)
	}
}
