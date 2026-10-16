package httpx

// The consumer's half of the generation rule. kit/cache's suite can only order the
// interleaving of its own two commands; this is the one that happens in here, where
// a miss is followed by a query and the query is the slow part:
//
//	Get (miss, stamped) → Tenants.ByHost → Set
//
// A suspension whose Move lands during the query must close the answer that query
// is bringing back. If it does not, the installation has stopped believing a host
// and every replica goes on reading the tenant it loaded before, for hostTTL. The
// write is stamped by the read that decided the load, which is why Set needs no
// read of its own to be honest — and this case is the only place that stamp is
// observed crossing from one call to the next.

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

// gated is a TenantLoader that says when it started its query and waits to be let
// go, so the move lands inside the load rather than in the hope that two goroutines
// meet. The suite always hands it a release that is either closed already or closed
// by the test while the query is held.
type gated struct {
	nothing
	tenant  tenancy.Tenant
	entered chan struct{}
	release chan struct{}
}

func (g *gated) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	g.entered <- struct{}{}
	<-g.release
	return g.tenant, nil
}

// TestAResolutionLoadedAcrossAMoveIsNotBelieved is the resurrection, refused.
func TestAResolutionLoadedAcrossAMoveIsNotBelieved(t *testing.T) {
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

	// The load begins: the miss has been read and stamped, and the query is out.
	suspended := hostScope.Entry("suspended.example")
	resolved := make(chan tenancy.Tenant, 1)
	go func() {
		tenant, err := a.resolve(ctx, "suspended.example")
		if err != nil {
			t.Errorf("resolve across the suspension: %v", err)
		}
		resolved <- tenant
	}()
	<-loader.entered

	// The operator suspends the host while the query is out, and the namespace
	// moves. Nothing orders this against the write that follows: the stamp does.
	if err := shared.Move(ctx, hostScope); err != nil {
		t.Fatalf("Move during the load: %v", err)
	}
	close(loader.release)

	// The request that was already waiting is served — the loader is the truth, and
	// a cache that turns an in-flight request into a refusal is the wrong way round.
	// What it must not do is leave that answer behind for every other reader.
	if got := <-resolved; got.ID != acme.ID {
		t.Fatalf("resolve answered %v, want the tenant the loader returned", got.ID)
	}
	if _, found, _, err := shared.Get(ctx, suspended); err != nil || found {
		t.Errorf("the resolution the installation had just stopped believing was left in the shared store: found=%v err=%v", found, err)
	}

	// The control, same wiring and no move in the middle of its load: the write
	// happens and the store believes it. Without this the case above would pass if
	// httpx had simply stopped caching resolutions at all.
	loader.release = make(chan struct{})
	close(loader.release)
	if _, err := a.resolve(ctx, "kept.example"); err != nil {
		t.Fatalf("resolve with nothing in flight: %v", err)
	}
	if _, found, _, err := shared.Get(ctx, hostScope.Entry("kept.example")); err != nil || !found {
		t.Fatalf("a resolution loaded with no move in progress never reached the shared store: found=%v err=%v; the write is gone, not merely refused", found, err)
	}
}
