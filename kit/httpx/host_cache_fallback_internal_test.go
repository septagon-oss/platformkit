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

// resolverOver is one replica: an API over the deployment's store, its own loader
// and the schema every resolution runs its query inside. The schema is the caller's,
// because the gap this file measures is between an entry being written and being
// read, and creating one — a schema drop and the whole migration — is the slowest
// thing a test here does: measured at 24 s for one run while the suite is in flight.
// hostTTL is 30 s, so a replica built between the two resolves puts a migration
// inside the entry's own lifetime and the case then measures the machine.
func resolverOver(t *testing.T, app *db.Conn, store cache.Cache, loader TenantLoader) *API {
	t.Helper()
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
	_, app := dbtest.Schema(t)
	a := resolverOver(t, app, down{}, &fixed{tenant: acme})
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
	// Both replicas exist before anything is written, so the only thing between the
	// write and the read is the read. That order is main's (T-0279, 0572dad), and its
	// reason is main's measurement: while the whole suite was in flight the two
	// fixtures' migrations started 111 s apart — every one of the thirty migration
	// files they logged answered in under 525 ms — and the case cost 218.89 s, against
	// a host believed for hostTTL, 30 s (tenant.go:28). Writing before the reader
	// exists spends the entry's whole life inside a wait nobody asked the case to make,
	// and the read below lands as "no tenant at this host" over a store that had
	// nothing wrong with it. One schema, shared, is the same cure taken one step
	// further: with a single migration there is no second fixture left to queue.
	_, app := dbtest.Schema(t)
	writer := resolverOver(t, app, shared, &fixed{tenant: installation})
	// The reader's own loader knows nothing: what it resolves came from the store.
	reader := resolverOver(t, app, shared, &fixed{err: tenancy.ErrNoSuchHost})

	written := time.Now()
	if _, err := writer.resolve(context.Background(), "ops.example"); err != nil {
		t.Fatalf("resolve through the writer: %v", err)
	}
	got, err := reader.resolve(context.Background(), "ops.example")
	if err != nil {
		// How far apart the write and the read were, because two different failures
		// reach this line and only one of them is this repository's: an entry that
		// never arrived is a bug, and an entry that passed its lifetime says the
		// machine spent hostTTL between the two resolves — which is worth saying, as
		// the one thing that can still spend it here is the machine.
		t.Fatalf("resolve through the second replica: %v; the entry the first wrote was not read (the write and the read were %s apart, and hostTTL is %s)",
			err, time.Since(written).Round(time.Millisecond), hostTTL)
	}
	if !reflect.DeepEqual(got, installation) {
		t.Errorf("the second replica resolved %+v, want the whole tenant %+v", got, installation)
	}
}
