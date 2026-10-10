package httpx_test

// public_write_queued_test.go pins what this kernel does with each of the four
// answers kit/limit gives, one row per answer, in kit/limit/README.md's order.
// The rows are the whole of the failure mode: a counted attempt, an attempt
// queued behind its own key, an attempt whose wall ran out, and a store that
// could not be reached at all. Two of them refuse the request, one admits it and
// one is an outage this middleware fails open on — and the distinction the four
// exist to make is the one a CI run got wrong, when a queue of same-key
// submissions came back as an error and the error was answered by admitting all
// 75 of them (T-0126 round 83, CI run 52994 job 53535).
//
// The fake holds an answer rather than computing one: kit/limit's own cases make
// the store produce these four, and TestAPublicWriteBurstAtOneDoorStopsAtTheWindow
// runs this same door against the real counter, so the fake's cases are the
// service's cases and not a second opinion about them. What is decided here is
// only the door's reading of an answer it has been handed.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// answeredLimit is one of kit/limit's four rows, held still: what it returns is
// what the counter returns in that world, including ErrBusy where the counter
// puts it (Count and Forget) and no error at all where Allow puts the refusal.
type answeredLimit struct {
	ok    bool
	retry time.Duration
	err   error
}

func (a answeredLimit) Allow(context.Context, string, int, time.Duration) (bool, time.Duration, error) {
	return a.ok, a.retry, a.err
}

// writeDoor composes the one public POST this file needs, counted by l, and
// counts the requests that reached the handler.
func writeDoor(t *testing.T, l httpx.WriteLimiter) (router http.Handler, door string, reached *atomic.Int64) {
	t.Helper()
	_, conn := dbtest.Schema(t)
	reached = &atomic.Int64{}
	api, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: host, Installation: installationHost, Conn: conn,
		Tenants: loaderFunc(func(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
			return tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return false, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		WriteLimiter: l,
		Log:          slog.New(slog.DiscardHandler),
	})
	httpx.Register(api.Surfaces("queued").Public, huma.Operation{
		OperationID: "queued-ask", Method: http.MethodPost, Path: "/ask",
	}, httpx.Public(), func(context.Context, *struct{}) (*body, error) {
		reached.Add(1)
		return &body{}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the fixture does not describe itself: %v", err)
	}
	return router, api.Surfaces("queued").Public.Path("/ask"), reached
}

// knock sends the anonymous POST the browser sends.
func knock(t *testing.T, router http.Handler, door string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://"+host+door, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}

// TestThePublicWriteDoorAnswersEachOfTheLimitersFourOutcomes is the table. The
// first row is ordinary, the second and third are the cure, and the fourth is
// what must not have changed: a limiter that cannot be reached is an outage of
// the limiter's, and the form still works.
func TestThePublicWriteDoorAnswersEachOfTheLimitersFourOutcomes(t *testing.T) {
	for _, one := range []struct {
		name    string
		counter answeredLimit
		want    int
		reached int64
		after   string
	}{
		{
			name:    "counted, with headroom",
			counter: answeredLimit{ok: true},
			want:    http.StatusOK, reached: 1, after: "",
		},
		{
			name:    "the window is spent",
			counter: answeredLimit{retry: 30 * time.Second},
			want:    http.StatusTooManyRequests, reached: 0, after: "30",
		},
		{
			// A queued attempt answers with the whole window, because nothing read
			// the row and the whole window is the one figure that cannot understate
			// what is left of it. kit/limit/README.md owns that choice.
			name:    "queued behind its own key",
			counter: answeredLimit{retry: anonWriteWindow},
			want:    http.StatusTooManyRequests, reached: 0, after: "60",
		},
		{
			name:    "the store could not be reached",
			counter: answeredLimit{err: errors.New("limit: dial tcp: connection refused")},
			want:    http.StatusOK, reached: 1, after: "",
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			router, door, reached := writeDoor(t, one.counter)
			res := knock(t, router, door)
			if res.Code != one.want {
				t.Fatalf("%s: the door answered %d, want %d: %s", one.name, res.Code, one.want, res.Body.String())
			}
			if got := reached.Load(); got != one.reached {
				t.Errorf("%s: the handler was reached %d times, want %d", one.name, got, one.reached)
			}
			if got := res.Header().Get("Retry-After"); got != one.after {
				t.Errorf("%s: Retry-After is %q, want %q", one.name, got, one.after)
			}
			if one.want == http.StatusTooManyRequests && !strings.Contains(res.Body.String(), httpx.CodeLimitExhausted) {
				t.Errorf("%s: the refusal carries no %s for the caller to read: %s", one.name, httpx.CodeLimitExhausted, res.Body.String())
			}
		})
	}
}

// anonWriteWindow is the window this kernel counts anonymous writes in, spelled
// here rather than read: publicWriteWindow is unexported, and this case is about
// the door. The burst case in package httpx reads both figures from inside the
// package, which is why the figure is stated once where it can be read.
const anonWriteWindow = time.Minute
