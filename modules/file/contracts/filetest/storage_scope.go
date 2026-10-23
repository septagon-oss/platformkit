package filetest

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

func storageTenantIsolation(t *testing.T, fresh func(*testing.T) StorageFixture) {
	a, b := fresh(t), fresh(t)
	if a.Scope.TenantID() == b.Scope.TenantID() {
		t.Fatal("the fixture handed two subtests the same tenant")
	}
	key := contracts.Key(uuid.NewString())
	storagePut(t, a.Storage, a.Scope, key, "the first tenant's bytes")
	// A key is a UUID this module minted, so another tenant asking for it
	// is asking for something that does not exist in their own prefix — the
	// same answer as a key nobody ever minted, and never the first tenant's
	// bytes.
	storageMissing(t, b.Storage, b.Scope, key)
	if err := b.Storage.Delete(t.Context(), b.Scope, key); err != nil {
		t.Fatalf("deleting another tenant's key: %v", err)
	}
	if got := storageRead(t, a.Storage, a.Scope, key); got != "the first tenant's bytes" {
		t.Fatalf("another tenant's delete reached these bytes: %q", got)
	}
	// Two tenants may hold the same key, because a key is a UUID inside a
	// prefix and the prefix is not theirs to name.
	storagePut(t, b.Storage, b.Scope, key, "the second tenant's bytes")
	if got := storageRead(t, a.Storage, a.Scope, key); got != "the first tenant's bytes" {
		t.Fatal("one tenant's upload replaced another's at the same key")
	}
}

func storageRequiresTenant(t *testing.T, fresh func(*testing.T) StorageFixture) {
	fixture := fresh(t)
	key := contracts.Key(uuid.NewString())
	if err := fixture.Storage.Put(t.Context(), contracts.Scope{}, key, strings.NewReader("x"), 1, contracts.Meta{}); !errors.Is(err, db.ErrNoTenant) {
		t.Fatalf("Put with the zero scope: %v, want db.ErrNoTenant", err)
	}
	if _, err := fixture.Storage.Get(t.Context(), contracts.Scope{}, key); !errors.Is(err, db.ErrNoTenant) {
		t.Fatalf("Get with the zero scope: %v, want db.ErrNoTenant", err)
	}
	if err := fixture.Storage.Delete(t.Context(), contracts.Scope{}, key); !errors.Is(err, db.ErrNoTenant) {
		t.Fatalf("Delete with the zero scope: %v, want db.ErrNoTenant", err)
	}
	storageMissing(t, fixture.Storage, fixture.Scope, key)
}

func storageValidatesKeys(t *testing.T, fresh func(*testing.T) StorageFixture) {
	fixture := fresh(t)
	// The whole traversal argument in one list. A Key is a defined string
	// type, so Key("../../etc/passwd") compiles, and the only thing between
	// that literal and the filesystem is this check — which is why every
	// implementation repeats it and the suite feeds it to each of them.
	for _, bad := range []string{"../../etc/passwd", "..", "", "x", "UPPERCASE", "a/b", "63://e/x", strings.ToUpper(uuid.NewString())} {
		k := contracts.Key(bad)
		if err := fixture.Storage.Put(t.Context(), fixture.Scope, k, strings.NewReader("x"), 1, contracts.Meta{}); !errors.Is(err, contracts.ErrInvalidKey) {
			t.Fatalf("Put(%q): %v, want ErrInvalidKey", bad, err)
		}
		if _, err := fixture.Storage.Get(t.Context(), fixture.Scope, k); !errors.Is(err, contracts.ErrInvalidKey) {
			t.Fatalf("Get(%q): %v, want ErrInvalidKey", bad, err)
		}
		if err := fixture.Storage.Delete(t.Context(), fixture.Scope, k); !errors.Is(err, contracts.ErrInvalidKey) {
			t.Fatalf("Delete(%q): %v, want ErrInvalidKey", bad, err)
		}
	}
}
