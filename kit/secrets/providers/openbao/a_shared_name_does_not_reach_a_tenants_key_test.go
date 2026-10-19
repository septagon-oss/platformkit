package openbao_test

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/secrets"
	"github.com/septagon-oss/platformkit/kit/secrets/providers/openbao"
)

// TestASharedNameDoesNotReachATenantsKey checks that a customer's ciphertext opens
// only for that customer, whatever the transit mount looks like. A tenant's key is
// mounted at `<tenant-uuid>-<name>`, and the installation's own key at its bare name,
// so the one spelling both address spaces can produce is a Shared name that begins
// with a tenant's UUID. A key created without `derived` — OpenBao's own default, and
// what an encrypt against an unmounted name creates when it carries no context —
// ignores the tenant's context, so the address is the only binding left, and a
// request that resolved another tenant must not be able to reach it by spelling it.
//
// It runs against the dev-mode container only: the wire stub folds the context into
// every key, so it cannot hold a key that ignores it.
func TestASharedNameDoesNotReachATenantsKey(t *testing.T) {
	url, token := os.Getenv("PLATFORMKIT_TEST_OPENBAO_URL"), os.Getenv("PLATFORMKIT_TEST_OPENBAO_TOKEN")
	if url == "" || token == "" {
		t.Skip("PLATFORMKIT_TEST_OPENBAO_URL and PLATFORMKIT_TEST_OPENBAO_TOKEN name the dev-mode OpenBao `make up` starts; neither is set, so a key that ignores its context cannot be mounted here")
	}
	store := mustStore(t, config.Secrets{
		Adapter: "openbao", URL: url, Token: token,
		KVMount: openbao.DefaultKVMount, TransitMount: openbao.DefaultTransitMount,
	})
	client := &http.Client{Timeout: 10 * time.Second}
	plaintext := []byte("tenant A's relay password")

	// The installation's own key, mounted as a deployment mounts one, still seals and
	// opens from any context: a cure that refused every Shared key would pass the
	// cases below by breaking the port.
	t.Run("the installation's own key still seals and opens", func(t *testing.T) {
		own := secrets.Shared("installation-" + uuid.NewString()[:8])
		put(t, client, url, token, openbao.DefaultTransitMount+"/keys/"+openbao.KeyAddress(own),
			map[string]any{"type": "aes256-gcm96"})
		sealed, err := store.Seal(context.Background(), own, plaintext)
		if err != nil {
			t.Fatalf("Seal under the installation's key: %v", err)
		}
		got, err := store.Open(tenantContext(uuid.New()), own, sealed)
		if err != nil || !bytes.Equal(got, plaintext) {
			t.Errorf("Open under the installation's key = %q, %v; want the plaintext", got, err)
		}
	})

	// An operator who mounts a customer's key with `bao write -f transit/keys/<name>`
	// gets a key that is not derived.
	t.Run("a customer's key mounted without derivation", func(t *testing.T) {
		tenantA, tenantB := uuid.New(), uuid.New()
		key := secrets.Of(tenantA, "relay")
		put(t, client, url, token, openbao.DefaultTransitMount+"/keys/"+openbao.KeyAddress(key),
			map[string]any{"type": "aes256-gcm96"})
		sealed, err := store.Seal(tenantContext(tenantA), key, plaintext)
		if err != nil {
			t.Fatalf("tenant A sealing under its own key: %v", err)
		}
		if got, err := store.Open(tenantContext(tenantA), key, sealed); err != nil || !bytes.Equal(got, plaintext) {
			t.Fatalf("tenant A opening its own seal = %q, %v; want the plaintext", got, err)
		}
		spelled := secrets.Shared(tenantA.String() + "-relay")
		if got, err := store.Open(tenantContext(tenantB), spelled, sealed); err == nil || len(got) != 0 {
			t.Errorf("tenant B opened tenant A's seal under %s: %q, %v; want a refusal and no bytes", spelled, got, err)
		}
	})

	// Nobody mounts anything: tenant B's request seals once under the spelling of
	// tenant A's key address before tenant A ever seals.
	t.Run("a customer's key first named by another tenant's request", func(t *testing.T) {
		tenantA, tenantB := uuid.New(), uuid.New()
		key := secrets.Of(tenantA, "relay")
		spelled := secrets.Shared(tenantA.String() + "-relay")
		_, _ = store.Seal(tenantContext(tenantB), spelled, []byte("anything"))
		sealed, err := store.Seal(tenantContext(tenantA), key, plaintext)
		if err != nil {
			// A store that refuses to seal under a key nobody mounted has nothing for
			// tenant B to open, which is a correct answer to this case.
			t.Logf("tenant A's seal under an unmounted key was refused: %v", err)
			return
		}
		if got, err := store.Open(tenantContext(tenantB), spelled, sealed); err == nil || len(got) != 0 {
			t.Errorf("tenant B opened tenant A's seal under %s: %q, %v; want a refusal and no bytes", spelled, got, err)
		}
	})
}
