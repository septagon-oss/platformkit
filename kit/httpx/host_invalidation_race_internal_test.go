package httpx

// The suspension route's own invalidation, against the race kit/cache's Move was
// written for. TestAResolutionLoadedAcrossAMoveIsNotBelieved proves the stamp
// closes a load when the namespace is moved; modules/tenant does not move the
// namespace, it calls InvalidateHost. This case runs that call in the same place
// in the same interleaving: a replica has missed and is loading the host when the
// operator's replica commits the suspension and forgets the host.

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestAnInvalidationDuringALoadLeavesNoResolutionBehind: the answer a load began
// before the suspension may serve the request that was waiting for it, and must
// not be what every replica reads for the rest of hostTTL.
func TestAnInvalidationDuringALoadLeavesNoResolutionBehind(t *testing.T) {
	_, app := dbtest.Schema(t)
	shared := cache.Memory("pkit")
	acme := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	loader := &gated{
		tenant:  acme,
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	a, _ := New(Options{
		Cache:     shared,
		Tenants:   loader,
		Conn:      app,
		Authorize: nothing{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	ctx := context.Background()

	resolved := make(chan error, 1)
	go func() {
		_, err := a.resolve(ctx, "suspended.example")
		resolved <- err
	}()
	<-loader.entered

	// What modules/tenant's suspend route runs once its write has committed.
	if err := a.InvalidateHost("suspended.example"); err != nil {
		t.Fatalf("InvalidateHost during the load: %v", err)
	}
	close(loader.release)
	if err := <-resolved; err != nil {
		t.Fatalf("resolve across the suspension: %v", err)
	}

	if _, found, _, err := shared.Get(ctx, hostScope.Entry("suspended.example")); err != nil || found {
		t.Errorf("the suspended host's resolution was written back after InvalidateHost and every replica believes it for hostTTL: found=%v err=%v", found, err)
	}

	// The control: a load with no invalidation in flight is stored and believed,
	// so the case above cannot pass by httpx no longer caching resolutions.
	if _, err := a.resolve(ctx, "kept.example"); err != nil {
		t.Fatalf("resolve with nothing in flight: %v", err)
	}
	if _, found, _, err := shared.Get(ctx, hostScope.Entry("kept.example")); err != nil || !found {
		t.Fatalf("a resolution loaded with no invalidation in flight never reached the shared store: found=%v err=%v", found, err)
	}
}
