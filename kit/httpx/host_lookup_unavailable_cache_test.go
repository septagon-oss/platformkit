package httpx_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// unavailableCacheReads retains the real store's writes and invalidations while
// its read requests fail, as requests to a shared store can fail independently.
type unavailableCacheReads struct{ cache.Cache }

func (unavailableCacheReads) Get(context.Context, cache.Key) ([]byte, bool, cache.Generation, error) {
	return nil, false, cache.Generation{}, errors.New("the cache read did not complete")
}

func TestARequestAfterInvalidationDoesNotJoinAnUnstampedHostLookup(t *testing.T) {
	_, conn := dbtest.Schema(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tenantID := uuid.New()
	entered := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	var held, changed atomic.Bool
	store := unavailableCacheReads{Cache: cache.Memory("pkit")}
	t.Cleanup(func() { _ = store.Close() })
	api, router := httpx.New(httpx.Options{
		Conn: conn, Cache: store, Installation: host, PublicHost: host,
		Tenants: loaderFunc(func(ctx context.Context, _ db.Tx[db.System], _ string) (tenancy.Tenant, error) {
			tag := "en"
			if changed.Load() {
				tag = "pt-PT"
			}
			snapshot := tenancy.Tenant{ID: tenantID, Languages: &tenancy.Languages{Default: tag}}
			if held.CompareAndSwap(false, true) {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return tenancy.Tenant{}, ctx.Err()
				}
			}
			return snapshot, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) { return true, nil }),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: tenantID}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	surface := api.Surfaces("tenant")
	httpx.Register(surface.App, huma.Operation{OperationID: "tenant-current-language", Method: http.MethodGet, Path: "/current"},
		httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*body, error) {
			tenant, _ := tenancy.FromContext(ctx)
			out := new(body)
			out.Body.Tenant = tenant.Languages.Default
			return out, nil
		})
	read := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+surface.App.Path("/current"), nil).WithContext(ctx)
		req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- read() }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("the first host lookup did not start")
	}
	changed.Store(true)
	if err := api.InvalidateHost(host); err != nil {
		t.Fatalf("invalidate the changed declaration: %v", err)
	}
	next := make(chan *httptest.ResponseRecorder, 1)
	go func() { next <- read() }()
	var after *httptest.ResponseRecorder
	select {
	case after = <-next:
		unblock()
	case <-time.After(300 * time.Millisecond):
		unblock()
		select {
		case after = <-next:
		case <-ctx.Done():
			t.Fatal("the request after invalidation did not finish")
		}
	case <-ctx.Done():
		t.Fatal("the request after invalidation did not start")
	}
	select {
	case before := <-first:
		if before.Code != http.StatusOK {
			t.Fatalf("inflight request = %d: %s", before.Code, before.Body.String())
		}
	case <-ctx.Done():
		t.Fatal("the first lookup did not finish")
	}
	if after.Code != http.StatusOK || !strings.Contains(after.Body.String(), `"tenant":"pt-PT"`) {
		t.Errorf("request after a successful invalidation with cache reads unavailable = %d: %s; want the current pt-PT declaration", after.Code, after.Body.String())
	}
}
