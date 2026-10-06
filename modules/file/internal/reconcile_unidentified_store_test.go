package internal_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// The memory fixture supplies storage and removal; this wrapper lists the two
// blobs as aged so the job reaches its ownership decision without waiting an hour.
type agedUnidentifiedStore struct {
	*filetest.Memory
	listed []contracts.Blob
}

func (s *agedUnidentifiedStore) Blobs(context.Context, db.Tx[db.System], time.Time) ([]contracts.Blob, error) {
	return s.listed, nil
}

func TestReconcileKeepsAnotherAppsBlobWhenTheStoreNamesNoApp(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	owned, foreign := uuid.New(), uuid.New()
	err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec(`INSERT INTO tenants (id, slug, name, app) VALUES
			(?, 'owned', 'Owned', ''), (?, 'foreign', 'Foreign', 'beta')`, owned, foreign).Error
	})
	if err != nil {
		t.Fatal(err)
	}

	store := &agedUnidentifiedStore{Memory: filetest.NewMemory()}
	put := func(tenant uuid.UUID) string {
		t.Helper()
		scope := filetest.TenantScope(t, tenant)
		key := contracts.Key(uuid.NewString())
		if err := store.Put(t.Context(), scope, key, strings.NewReader("orphan"), -1, contracts.Meta{}); err != nil {
			t.Fatal(err)
		}
		name, err := scope.ObjectName(key)
		if err != nil {
			t.Fatal(err)
		}
		store.listed = append(store.listed, contracts.Blob{TenantID: tenant, Key: key})
		return name
	}
	ownName := put(owned)
	foreignName := put(foreign)

	sweep := internal.NewReconcile(store, time.Second)
	sweep.Use(dbtest.SystemToken())
	if err := sweep.Jobs()[0].Run(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	keys := store.Keys()
	if slices.Contains(keys, ownName) {
		t.Error("the sweep left its own unreferenced blob")
	}
	if !slices.Contains(keys, foreignName) {
		t.Error("the sweep removed a blob whose tenant another app holds")
	}
}
