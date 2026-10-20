package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// erroredCachePayload models a read that returned bytes before reporting failure.
// Its remaining methods use the normal memory adapter.
type erroredCachePayload struct {
	cache.Cache
	payload []byte
	under   cache.Generation
}

func (c erroredCachePayload) Get(context.Context, cache.Key) ([]byte, bool, cache.Generation, error) {
	return c.payload, true, c.under, errDown
}

func TestAFailedCacheReadCannotChooseTheTenantFromItsPayload(t *testing.T) {
	stale := tenancy.Tenant{
		ID: uuid.New(), Slug: "installation", Operator: true,
		Languages: &tenancy.Languages{Default: "en"},
	}
	current := tenancy.Tenant{
		ID: uuid.New(), Slug: "customer",
		Languages: &tenancy.Languages{Default: "pt-PT", Others: []string{"en"}},
	}
	payload, err := json.Marshal(newCachedTenant(stale))
	if err != nil {
		t.Fatal(err)
	}
	backend := cache.Memory("pkit")
	t.Cleanup(func() { _ = backend.Close() })
	_, _, under, err := backend.Get(t.Context(), hostScope.Entry("customer.example"))
	if err != nil {
		t.Fatal(err)
	}
	store := erroredCachePayload{Cache: backend, payload: payload, under: under}
	loader := &fixed{tenant: current}
	api := resolverOver(t, store, loader)

	got, err := api.resolve(t.Context(), "customer.example")
	if err != nil || !reflect.DeepEqual(got, current) {
		t.Fatalf("cache read failed: got %+v, error %v; want the loader's customer %+v", got, err, current)
	}

	for _, failure := range []error{tenancy.ErrNoSuchHost, errDown} {
		loader.tenant, loader.err = tenancy.Tenant{}, failure
		got, err = api.resolve(t.Context(), "customer.example")
		if !errors.Is(err, failure) || !reflect.DeepEqual(got, tenancy.Tenant{}) {
			t.Errorf("loader refused with %v: got %+v, error %v; want no tenant and the loader's error", failure, got, err)
		}
	}
}
