package httpx_test

// public_read_uncounted_test.go pins the line that keeps the public surface's
// reads out of the write counter: the limiter bounds what an anonymous visitor
// may submit, never what they may see. The first thing a workspace shows — its
// name, logo and colour before sign-in — is an anonymous GET, and kit/app
// composes the Postgres counter into every deployment, so a read that was
// counted would spend a visitor's window on looking and a crawler would spend
// everyone's.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

// refusingSpentCount refuses every attempt it is asked about and counts the
// asking, so a read that reached it would be seen twice over: the 429 it would
// be answered with, and the consultation the door never owed it.
type refusingSpentCount struct {
	consulted atomic.Int64
}

func (l *refusingSpentCount) Allow(context.Context, string, int, time.Duration) (bool, time.Duration, error) {
	l.consulted.Add(1)
	return false, time.Minute, nil
}

// TestAPublicReadIsNeverSpentAgainstTheWriteLimit knocks at a public GET door
// behind a limiter that refuses everything: the read is answered 200, the
// handler is reached once, and the counter is never consulted.
func TestAPublicReadIsNeverSpentAgainstTheWriteLimit(t *testing.T) {
	_, conn := dbtest.Schema(t)
	limiter := &refusingSpentCount{}
	var reached atomic.Int64
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
		WriteLimiter: limiter,
		Log:          slog.New(slog.DiscardHandler),
	})
	httpx.Register(api.Surfaces("reading").Public, huma.Operation{
		OperationID: "reading-face", Method: http.MethodGet, Path: "/face",
	}, httpx.Public(), func(context.Context, *struct{}) (*body, error) {
		reached.Add(1)
		return &body{}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the fixture does not describe itself: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://"+host+api.Surfaces("reading").Public.Path("/face"), nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("an anonymous read with the counter refusing everything = %d %s, want 200", res.Code, res.Body.String())
	}
	if got := reached.Load(); got != 1 {
		t.Errorf("the handler was reached %d times, want 1", got)
	}
	if got := limiter.consulted.Load(); got != 0 {
		t.Errorf("the write counter was consulted %d times by a safe method, want 0", got)
	}
}
