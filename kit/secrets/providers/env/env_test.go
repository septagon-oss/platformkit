package env_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/secrets"
	"github.com/septagon-oss/platformkit/kit/secrets/providers/env"
	"github.com/septagon-oss/platformkit/kit/secrets/secretstest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// tenant is one customer, for the case this adapter refuses.
var tenant = uuid.MustParse("11111111-1111-4111-8111-111111111111")

// TestTheEnvironmentStoreConforms runs the shared suite over a source a test drives,
// minus the two cases a flat environment cannot answer: a version, and a tenant's
// address. Both refusals are asserted below, positively, rather than left as absent
// cases.
func TestTheEnvironmentStoreConforms(t *testing.T) {
	vars := map[string]string{}
	adapter := secretstest.Adapter{
		Name:  "env",
		Store: func(*testing.T) secrets.Store { return mustStore(t, vars) },
		Failing: func(*testing.T) secrets.Store {
			store, err := env.New(func(string) (string, error) {
				return "", errors.New("the environment is not readable")
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			return store
		},
		Write: func(t *testing.T, ref secrets.Ref, plaintext []byte) {
			t.Helper()
			vars[variableOf(ref)] = string(plaintext)
		},
		WriteKey: func(t *testing.T, ref secrets.Ref) {
			t.Helper()
			vars[variableOf(ref)] = secretstest.KeyMaterial
		},
		// An environment variable is a Go string: every byte survives.
		Exact: []byte{0x00, 0xff, 0xfe, 'a', 0x00, 0x7f, 0xc3, 0x28, '\n'},
	}
	secretstest.Conformance(t, adapter)
	secretstest.Keys(t, adapter)
}

// TestAnUnsetVariableIsNotFoundAndNotAnEmptyValue is the one mistake an env adapter
// can make that no other adapter can: answering ("", nil) for a name nobody set,
// which turns a missing credential into an empty password and a relay's 535 into a
// mystery. modules/auth's Lookup is the precedent this port refuses to copy.
func TestAnUnsetVariableIsNotFoundAndNotAnEmptyValue(t *testing.T) {
	store := mustStore(t, map[string]string{})
	secret, err := store.Get(context.Background(), secrets.Shared("mail.password"))
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("Get of an unset variable = %q, %v; want ErrNotFound", secret.Bytes, err)
	}
	if len(secret.Bytes) != 0 {
		t.Errorf("an unset variable answered bytes: %q", secret.Bytes)
	}
	if _, err := store.Version(context.Background(), secrets.Shared("mail.password")); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("Version of an unset variable = %v; want ErrNotFound", err)
	}
}

// TestTheMappingIsTheVariableTheOperatorAlreadySet is the whole of why this move is
// invisible: mail.password is PLATFORMKIT_MAIL_PASSWORD, the same name kit/config
// read until this package existed.
func TestTheMappingIsTheVariableTheOperatorAlreadySet(t *testing.T) {
	t.Setenv("PLATFORMKIT_MAIL_PASSWORD", "from the environment\n")
	store, err := env.FromOS()
	if err != nil {
		t.Fatalf("FromOS: %v", err)
	}
	secret, err := store.Get(context.Background(), secrets.Shared("mail.password"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// Exactly the bytes, newline included: the port decides nothing about protocols,
	// and the SMTP carrier is the layer that knows a password never ends in one.
	if string(secret.Bytes) != "from the environment\n" {
		t.Errorf("Get = %q; want the value untouched", secret.Bytes)
	}
	if version, _ := store.Version(context.Background(), secrets.Shared("mail.password")); version != "" {
		t.Errorf("Version = %q; an environment variable has no token, and inventing one would be a digest of a low-entropy secret", version)
	}
}

// TestATenantsAddressIsRefusedNotAnswered is the limit stated as an answer: the flat
// process environment cannot hold one value per customer, and the fix is the
// deployment's (mount file, set secrets.adapter openbao) rather than a name that
// quietly means somebody else's password.
func TestATenantsAddressIsRefusedNotAnswered(t *testing.T) {
	store := mustStore(t, map[string]string{"PLATFORMKIT_MAIL_PASSWORD": "installation"})
	ctx := contextWith(tenant)
	_, err := store.Get(ctx, secrets.Of(tenant, "mail.password"))
	if !errors.Is(err, env.ErrNotSupported) {
		t.Errorf("a tenant's secret = %v; want ErrNotSupported", err)
	}
	if errors.Is(err, secrets.ErrNotFound) {
		t.Error("ErrNotSupported wears ErrNotFound; an operator would go and mount a variable that cannot mean it")
	}
}

// TestNoCredentialReachesASentence: an adapter's error reaches a log line, and the
// name it names is the variable, never the value read out of it.
func TestNoCredentialReachesASentence(t *testing.T) {
	const secretValue = "hunter2-never-in-a-log"
	store := mustStore(t, map[string]string{"PLATFORMKIT_MAIL_PASSWORD": secretValue})
	_, err := store.Get(context.Background(), secrets.Shared("smtp.password"))
	if err == nil {
		t.Fatal("an absent name answered")
	}
	if strings.Contains(err.Error(), secretValue) {
		t.Errorf("the refusal carries the credential: %v", err)
	}
	if !strings.Contains(err.Error(), "PLATFORMKIT_SMTP_PASSWORD") {
		t.Errorf("the refusal does not name the variable an operator would set: %v", err)
	}
}

// mustStore is the adapter over a map a case fills in, which is what a Source is
// for: the two answers an adapter must not confuse are unset and unreadable.
func mustStore(t *testing.T, vars map[string]string) secrets.Store {
	t.Helper()
	store, err := env.New(func(name string) (string, error) {
		value, ok := vars[name]
		if !ok {
			return "", secrets.ErrNotFound
		}
		return value, nil
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store
}

// contextWith is a context that resolved one customer, which is the half of the read
// the port's scope check reads before this adapter's own refusal answers.
func contextWith(tenant uuid.UUID) context.Context {
	return tenancy.WithTenant(context.Background(), tenancy.Tenant{ID: tenant, Slug: "tenant-" + tenant.String()[:8]})
}

// variableOf is the adapter's mapping, spelled out here rather than reached into:
// a fixture that wrote variables by the adapter's own private function would pass
// even if the mapping changed, and the mapping is the promise an operator reads.
func variableOf(ref secrets.Ref) string {
	mapped := strings.NewReplacer(".", "_", "-", "_").Replace(ref.Name())
	return "PLATFORMKIT_" + strings.ToUpper(mapped)
}
