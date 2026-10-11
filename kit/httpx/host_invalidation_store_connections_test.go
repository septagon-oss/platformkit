package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/cache/providers/valkey"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestAnInvalidationClosesALoadAcrossStoreConnections holds the ordering of a
// host lookup and an invalidation across two separate connections to one store.
func TestAnInvalidationClosesALoadAcrossStoreConnections(t *testing.T) {
	address := os.Getenv("PLATFORMKIT_TEST_VALKEY_URL")
	if address == "" {
		t.Skip("PLATFORMKIT_TEST_VALKEY_URL is unset; make up starts the shared store")
	}
	appName := "pkit-" + uuid.NewString()[:8]
	connect := func() cache.Cache {
		c, err := valkey.Connect(t.Context(), config.Cache{App: appName, URL: address})
		if err != nil {
			t.Fatalf("connect to the shared store: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	firstStore, secondStore := connect(), connect()
	_, conn := dbtest.Schema(t)
	loader := &gated{
		tenant:  tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"},
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	api := func(store cache.Cache) *API {
		a, _ := New(Options{
			Cache:     store,
			Tenants:   loader,
			Conn:      conn,
			Authorize: nothing{},
			Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
				return tenancy.Principal{}, false, nil
			},
			Log: slog.New(slog.DiscardHandler),
		})
		return a
	}
	first, second := api(firstStore), api(secondStore)
	ctx := t.Context()
	loaded := make(chan error, 1)
	go func() {
		_, err := first.resolve(ctx, "suspended.example")
		loaded <- err
	}()
	released := false
	defer func() {
		if !released {
			close(loader.release)
		}
	}()
	select {
	case <-loader.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first connection did not enter the host loader")
	}
	if err := second.InvalidateHost("suspended.example"); err != nil {
		t.Fatalf("invalidate from the second connection: %v", err)
	}
	close(loader.release)
	released = true
	if err := <-loaded; err != nil {
		t.Fatalf("finish the lookup that began before invalidation: %v", err)
	}
	if _, found, _, err := secondStore.Get(ctx, hostScope.Entry("suspended.example")); err != nil || found {
		t.Errorf("the second connection believes a resolution loaded before invalidation: found=%v err=%v", found, err)
	}

	// A later lookup must still be shared, so the miss above cannot pass merely
	// because the two connections are isolated or writes have stopped.
	if _, err := second.resolve(ctx, "kept.example"); err != nil {
		t.Fatalf("resolve a host after invalidation: %v", err)
	}
	if _, found, _, err := firstStore.Get(ctx, hostScope.Entry("kept.example")); err != nil || !found {
		t.Errorf("the first connection did not see a later resolution: found=%v err=%v", found, err)
	}
}
