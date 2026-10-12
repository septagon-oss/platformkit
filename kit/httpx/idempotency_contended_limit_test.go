package httpx_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestStoredPostgresRefusalWaitExcludesTimeWaitingForTheCounterLock(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	f := &fixture{
		tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"},
		app:    conn, logs: &lines{},
	}
	counter := limit.Postgres(httpx.ConnFrom)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), f.tenant), conn)
	const window = 5 * time.Second
	const name = "contended-entries"
	if allowed, _, _, err := counter.Reserve(ctx, name, 1, window); err != nil || !allowed {
		t.Fatalf("seed counter: allowed=%v error=%v", allowed, err)
	}
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: f, Conn: conn,
		Authorize: f, Authenticate: f.authenticate,
		Idempotency: idempotency.Postgres(httpx.ConnFrom), Log: slog.New(slog.DiscardHandler),
	})
	runs := 0
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "create-contended-entry", Method: http.MethodPost, Path: "/contended-entries",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		runs++
		allowed, remaining, left, err := counter.Reserve(ctx, name, 1, window)
		if err != nil {
			return nil, err
		}
		httpx.SayCounted(ctx, httpx.Counted{
			Name: name, Quota: 1, Window: window, Remaining: remaining, Left: left, Refused: !allowed,
		})
		return nil, problem.New(http.StatusTooManyRequests, "entry limit exhausted")
	})
	locker, err := admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Rollback()
	var lockerPID, count int
	if err := locker.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&lockerPID); err != nil {
		t.Fatal(err)
	}
	key := f.tenant.ID.String() + "/" + name
	if err := locker.QueryRowContext(t.Context(), "SELECT count FROM platformkit_limits WHERE key = $1 FOR UPDATE", key).Scan(&count); err != nil {
		t.Fatal(err)
	}
	const path = "/api/v1/public/probe/contended-entries"
	answers := make(chan *httptest.ResponseRecorder, 1)
	go func() { answers <- keyedPost(t, router, http.MethodPost, path, "contended-refusal", "{}") }()
	// Confirm a real counter statement is blocked before spending time under its lock.
	waitCtx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for {
		var blocked bool
		err := admin.QueryRowContext(waitCtx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))
			AND query LIKE '%INSERT INTO platformkit_limits%')`, lockerPID).Scan(&blocked)
		if err != nil {
			t.Fatalf("wait for contending counter: %v", err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(2100 * time.Millisecond)
	if err := locker.Rollback(); err != nil {
		t.Fatal(err)
	}
	var first *httptest.ResponseRecorder
	select {
	case first = <-answers:
	case <-time.After(10 * time.Second):
		t.Fatal("counter did not finish after its lock was released")
	}
	if first.Code != http.StatusTooManyRequests {
		t.Fatalf("first answer = %d %s", first.Code, first.Body.String())
	}
	var maximumWait int
	if err := admin.QueryRowContext(t.Context(), `SELECT count,
		GREATEST(0, ceil(extract(epoch FROM (window_start + interval '5 seconds' - clock_timestamp()))))::int
		FROM platformkit_limits WHERE key = $1`, key).Scan(&count, &maximumWait); err != nil {
		t.Fatal(err)
	}
	replay := keyedPost(t, router, http.MethodPost, path, "contended-refusal", "{}")
	if replay.Code != http.StatusTooManyRequests || replay.Body.String() != first.Body.String() || runs != 1 || count != 2 {
		t.Fatalf("replay=%d body=%q executions=%d counter=%d; want same refusal without another execution or reservation", replay.Code, replay.Body.String(), runs, count)
	}
	if got := replay.Header().Get(httpx.RateLimitPolicyHeader); got != `"contended-entries";q=1;w=5` {
		t.Errorf("replayed policy = %q; want the deciding policy", got)
	}
	wait, err := strconv.Atoi(replay.Header().Get(httpx.RetryAfterHeader))
	if err != nil || wait < 1 || wait > max(maximumWait, 1) {
		t.Errorf("replayed Retry-After = %q, Postgres counter has at most %d seconds left; waiting for its lock must not extend the stored window", replay.Header().Get(httpx.RetryAfterHeader), maximumWait)
	}
	var remaining, reset int
	if _, err := fmt.Sscanf(replay.Header().Get(httpx.RateLimitHeader), `"contended-entries";r=%d;t=%d`, &remaining, &reset); err != nil || remaining != 0 || reset < 0 || reset > maximumWait {
		t.Errorf("replayed RateLimit = %q; deciding counter has at most %d seconds left", replay.Header().Get(httpx.RateLimitHeader), maximumWait)
	}
}
