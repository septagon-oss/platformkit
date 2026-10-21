package openbao_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/secrets"
	"github.com/septagon-oss/platformkit/kit/secrets/providers/openbao"
)

func TestKeyAddressSpacesAreDisjoint(t *testing.T) {
	for _, first := range "0123456789abcdef" {
		tenant := uuid.MustParse(string(first) + "1111111-1111-4111-8111-111111111111")
		for _, name := range []string{"k", "k.1", "k_1", "k-1", strings.Repeat("k", 26)} {
			key := secrets.Of(tenant, name)
			alias := secrets.Shared(tenant.String() + "-" + name)
			if key.Name() == "" || alias.Name() == "" {
				t.Fatal("the two addresses must be legal port inputs")
			}
			if openbao.KeyAddress(key) == openbao.KeyAddress(alias) {
				t.Errorf("tenant key %s and installation key %s address the same transit key", key, alias)
			}
			other := secrets.Of(tenant, "other")
			if openbao.KeyAddress(key) == openbao.KeyAddress(other) {
				t.Errorf("distinct tenant keys %s and %s address the same transit key", key, other)
			}
		}
	}
}

func TestTenantScopeRefusalsNeverReachOpenBao(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	store := mustStore(t, config.Secrets{Adapter: "openbao", URL: server.URL, Token: "fixture-token"})
	ref := secrets.Of(tenantA, "relay")
	ctx := tenantContext(tenantB)
	if got, err := store.Get(ctx, ref); !errors.Is(err, secrets.ErrNoScope) || len(got.Bytes) != 0 || got.Version != "" {
		t.Errorf("cross-tenant Get = %v, %v; want ErrNoScope and no secret or version", got, err)
	}
	if got, err := store.Version(ctx, ref); !errors.Is(err, secrets.ErrNoScope) || got != "" {
		t.Errorf("cross-tenant Version = %q, %v; want ErrNoScope and no version", got, err)
	}
	if got, err := store.Seal(ctx, ref, []byte("credential")); !errors.Is(err, secrets.ErrNoScope) || len(got) != 0 {
		t.Errorf("cross-tenant Seal = %q, %v; want ErrNoScope and no envelope", got, err)
	}
	if got, err := store.Open(ctx, ref, []byte("vault:v1:YWJj")); !errors.Is(err, secrets.ErrNoScope) || len(got) != 0 {
		t.Errorf("cross-tenant Open = %q, %v; want ErrNoScope and no plaintext", got, err)
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("refused calls made %d requests to OpenBao; want none", got)
	}
}
