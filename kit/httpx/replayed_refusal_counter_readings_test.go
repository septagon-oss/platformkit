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

func TestAReplayedRefusalStatesEachDecidingCounterOnce(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	f := &fixture{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme"}, app: conn, logs: &lines{}}
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: f, Conn: conn,
		Authorize: f, Authenticate: f.authenticate,
		WriteLimiter: limit.Postgres(func(context.Context) (*db.Conn, bool) { return conn, true }),
		Idempotency:  idempotency.Postgres(httpx.ConnFrom), Log: slog.New(slog.DiscardHandler),
	})
	counter := limit.Postgres(httpx.ConnFrom)
	runs := 0
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "create-capped-entry", Method: http.MethodPost, Path: "/capped-entries",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		runs++
		allowed, remaining, left, err := counter.Reserve(ctx, "capped-entries", 0, 2*time.Second)
		if err != nil {
			return nil, err
		}
		httpx.SayCounted(ctx, httpx.Counted{
			Name: "capped-entries", Quota: 0, Window: 2 * time.Second,
			Remaining: remaining, Left: left, Refused: !allowed,
		})
		return nil, problem.New(http.StatusTooManyRequests, "entry limit exhausted")
	})
	const path = "/api/v1/public/probe/capped-entries"
	if seed := keyedPost(t, router, http.MethodPost, path, "", "{}"); seed.Code != http.StatusTooManyRequests {
		t.Fatalf("seed answer = %d %s", seed.Code, seed.Body.String())
	}
	// An existing public window can be near its end when a keyed write arrives.
	if _, err := admin.ExecContext(t.Context(), "UPDATE platformkit_limits SET window_start = clock_timestamp() - interval '59 seconds'"); err != nil {
		t.Fatal(err)
	}
	first := keyedPost(t, router, http.MethodPost, path, "capped-once", "{}")
	if first.Code != http.StatusTooManyRequests {
		t.Fatalf("first answer = %d %s; want the deciding refusal", first.Code, first.Body.String())
	}
	want := strings.Split(first.Header().Get(httpx.RateLimitHeader), ",")
	if len(want) != 2 {
		t.Fatalf("deciding refusal's readings = %q; want both counters", want)
	}
	longestWait := 0
	for _, original := range want {
		_, seconds, ok := strings.Cut(original, ";t=")
		wait, err := strconv.Atoi(seconds)
		if !ok || err != nil || wait < 0 {
			t.Fatalf("invalid original reading %q", original)
		}
		longestWait = max(longestWait, wait)
	}
	for _, after := range []struct {
		name  string
		pause time.Duration
	}{{"on the first retry", 0}, {"after both deciding windows close", time.Duration(longestWait)*time.Second + 100*time.Millisecond}} {
		t.Run(after.name, func(t *testing.T) {
			time.Sleep(after.pause)
			replay := keyedPost(t, router, http.MethodPost, path, "capped-once", "{}")
			if replay.Code != http.StatusTooManyRequests || replay.Body.String() != first.Body.String() || runs != 2 {
				t.Fatalf("replay status=%d body=%q executions=%d; want the stored refusal without another execution",
					replay.Code, replay.Body.String(), runs)
			}
			if got := replay.Header().Values(httpx.RateLimitPolicyHeader); len(got) != 1 || got[0] != first.Header().Get(httpx.RateLimitPolicyHeader) {
				t.Errorf("replayed policy = %q; want the deciding policy once", got)
			}
			if after.pause > 0 && replay.Header().Get(httpx.RetryAfterHeader) != "1" {
				t.Errorf("expired refusal's Retry-After = %q; want the one-second floor", replay.Header().Get(httpx.RetryAfterHeader))
			}
			got := strings.Split(replay.Header().Get(httpx.RateLimitHeader), ",")
			if len(got) != len(want) {
				t.Fatalf("replayed RateLimit = %q; deciding refusal stated %q; want one reading per deciding counter", got, want)
			}
			for i, original := range want {
				reading, seconds, ok := strings.Cut(original, ";t=")
				oldWait, err := strconv.Atoi(seconds)
				if !ok || err != nil {
					t.Fatalf("invalid original reading %q", original)
				}
				replayedReading, seconds, ok := strings.Cut(got[i], ";t=")
				wait, err := strconv.Atoi(seconds)
				if !ok || err != nil || replayedReading != reading || wait < 0 || wait > oldWait || (after.pause > 0 && wait != 0) {
					t.Errorf("replayed reading = %q; want %q with its original quota and elapsed wait deducted", got[i], original)
				}
			}
		})
	}
}
