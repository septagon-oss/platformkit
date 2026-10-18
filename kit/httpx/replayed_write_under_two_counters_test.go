package httpx_test

// A public write can be refused under two counters at once: the kernel's anonymous
// limit, which states its policy line on every reply this surface gives, and a cap a
// handler counts for itself. The retry of that refusal is answered from the record, and
// the ceilings it names are both of them, in one field. A policy line appended to the
// one the guard above wrote on this exchange would be two fields naming one ceiling
// twice, and a client that read the first of them would never hear of the counter that
// actually refused the write.

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

// TestAReplayedWriteUnderTwoCountersStatesOnePolicyLineAsItWasDecided refuses one
// keyed public write under the anonymous limit and the handler's own cap, then asks what
// the retry is told: the same ceilings, named once, with the reading restated against
// the moment the retry is answered at.
func TestAReplayedWriteUnderTwoCountersStatesOnePolicyLineAsItWasDecided(t *testing.T) {
	api, router, _ := keyedSetup(t, idempotency.Postgres(httpx.ConnFrom), limit.Memory())
	counter := limit.Memory()
	runs := 0
	const window = time.Hour
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "create-two-counter-entry", Method: http.MethodPost, Path: "/two-counter-entries",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		runs++
		allowed, remaining, left, err := counter.Reserve(ctx, "two-counter-entries", 0, window)
		if err != nil {
			return nil, err
		}
		httpx.SayCounted(ctx, httpx.Counted{
			Name: "two-counter-entries", Quota: 0, Window: window,
			Remaining: remaining, Left: left, Refused: !allowed,
		})
		return nil, problem.New(http.StatusTooManyRequests, "entry limit exhausted")
	})
	const path = "/api/v1/public/probe/two-counter-entries"
	first := keyedPost(t, router, http.MethodPost, path, "two-counters", "{}")
	if first.Code != http.StatusTooManyRequests {
		t.Fatalf("the first attempt = %d %s, want 429", first.Code, first.Body.String())
	}
	const want = `"public-writes";q=60;w=60,"two-counter-entries";q=0;w=3600`
	if got := first.Header().Values(httpx.RateLimitPolicyHeader); len(got) != 1 || got[0] != want {
		t.Fatalf("the deciding refusal named %q; want one field naming both ceilings, %q", got, want)
	}
	wait, err := strconv.Atoi(first.Header().Get(httpx.RetryAfterHeader))
	if err != nil {
		t.Fatalf("the deciding refusal's Retry-After = %q", first.Header().Get(httpx.RetryAfterHeader))
	}
	replay := keyedPost(t, router, http.MethodPost, path, "two-counters", "{}")
	if replay.Code != http.StatusTooManyRequests || replay.Body.String() != first.Body.String() || runs != 1 {
		t.Fatalf("replay = %d body=%q executions=%d; want the same refusal, from one execution",
			replay.Code, replay.Body.String(), runs)
	}
	if got := replay.Header().Values(httpx.RateLimitPolicyHeader); len(got) != 1 || got[0] != want {
		t.Errorf("replayed policy = %q; the deciding refusal named one field %q", got, want)
	}
	if got := replay.Header().Values(httpx.RateLimitHeader); len(got) != 1 ||
		!strings.Contains(got[0], `"public-writes"`) || !strings.Contains(got[0], `"two-counter-entries"`) {
		t.Errorf("replayed readings = %q; want both counters the answer was decided under, in one field", got)
	}
	got, err := strconv.Atoi(replay.Header().Get(httpx.RetryAfterHeader))
	if err != nil || got < 1 || got > wait {
		t.Errorf("replayed Retry-After = %q; the deciding refusal said %d seconds",
			replay.Header().Get(httpx.RetryAfterHeader), wait)
	}
}
