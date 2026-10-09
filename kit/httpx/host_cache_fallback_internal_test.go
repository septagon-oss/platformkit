package httpx

// The two promises tenant.go makes about the shared store and that no case held:
// a store that answers nothing costs a lookup and never a host, and an entry read
// back by another replica is the whole tenant the loader returned — Operator, which
// decides who reaches the control plane, and Languages, which decide the page.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// down is a store every call of which fails, the way an unreachable server does.
type down struct{}

var errDown = errors.New("the store is not answering")

func (down) Get(context.Context, cache.Key) ([]byte, bool, cache.Generation, error) {
	return nil, false, cache.Generation{}, errDown
}
func (down) Set(context.Context, cache.Key, []byte, time.Duration, cache.Generation) error {
	return errDown
}
func (down) Delete(context.Context, ...cache.Key) error { return errDown }
func (down) Move(context.Context, cache.Scope) error    { return errDown }
func (down) Close() error                               { return nil }

// fixed answers one tenant for every host, or the error it was given.
type fixed struct {
	nothing
	tenant tenancy.Tenant
	err    error
}

func (f *fixed) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return f.tenant, f.err
}

func resolverOver(t *testing.T, store cache.Cache, loader TenantLoader) *API {
	t.Helper()
	_, app := dbtest.Schema(t)
	a, _ := New(Options{
		Cache:     store,
		Tenants:   loader,
		Conn:      app,
		Authorize: nothing{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	return a
}

// TestAHostStillResolvesWhenTheStoreIsDown: the database is the truth for a
// resolution, so a store that answers nothing is one query and never a refusal.
func TestAHostStillResolvesWhenTheStoreIsDown(t *testing.T) {
	acme := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	a := resolverOver(t, down{}, &fixed{tenant: acme})
	got, err := a.resolve(context.Background(), "acme.example")
	if err != nil {
		t.Fatalf("resolve with the store down: %v; a cache outage refused a host the database knows", err)
	}
	if got.ID != acme.ID {
		t.Errorf("resolve with the store down = %v, want %v", got.ID, acme.ID)
	}
}

// TestACachedResolutionCarriesTheWholeTenant: the replica that reads the entry
// another wrote resolves the same tenant the loader gave the writer.
func TestACachedResolutionCarriesTheWholeTenant(t *testing.T) {
	shared := cache.Memory("pkit")
	installation := tenancy.Tenant{
		ID: uuid.New(), Slug: "installation", Name: "Installation", Operator: true,
		Languages: &tenancy.Languages{Default: "pt", Others: []string{"en"}},
	}
	writer := resolverOver(t, shared, &fixed{tenant: installation})
	// The reader's own loader knows nothing: what it resolves came from the store.
	//
	// Both fixtures exist before either resolve runs, and the order is the case's own. Every
	// resolverOver migrates a schema of its own, and migrations queue behind one composition
	// advisory lock across the whole server: in the whole-suite run of 2026-10-08 the two fixtures'
	// migrations started 111 s apart, while every one of the thirty migration files they logged
	// answered in under 525 ms (the case cost 218.89 s), and a host is believed for hostTTL —
	// 30 s (tenant.go:28). Writing before the reader is built spends the entry's whole life inside a
	// wait nobody asked the case to make, and the read below lands as "no tenant at this host" over
	// a store that had nothing wrong with it. The assertions are the case; this order only stops a
	// fixture from measuring the queue.
	reader := resolverOver(t, shared, &fixed{err: tenancy.ErrNoSuchHost})
	if _, err := writer.resolve(context.Background(), "ops.example"); err != nil {
		t.Fatalf("resolve through the writer: %v", err)
	}

	got, err := reader.resolve(context.Background(), "ops.example")
	if err != nil {
		t.Fatalf("resolve through the second replica: %v; the entry the first wrote was not read", err)
	}
	if !reflect.DeepEqual(got, installation) {
		t.Errorf("the second replica resolved %+v, want the whole tenant %+v", got, installation)
	}
}
