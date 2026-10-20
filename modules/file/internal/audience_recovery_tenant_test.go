package internal_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

func TestAudienceRecoveryIgnoresOtherTenantsUploadEvents(t *testing.T) {
	adminURL, appURL := dbtest.URLs(t)
	legacy := file.Migrations
	files := fstest.MapFS{}
	names, err := fs.Glob(legacy.Files, "*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		// The column under recovery arrives in 000064, so the layout this walks is the
		// schema as it stood before that file: every file below version 64, and nothing
		// at or above it. A later file of this source (000071 adds a column of its own)
		// would be built into the old schema and then refused when the real migration
		// ran — one version is one ledger row, applied in order.
		if version, _, _ := strings.Cut(name, "_"); version >= "000064" {
			continue
		}
		data, err := fs.ReadFile(legacy.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	legacy.Files = files
	if err := db.Migrate(t.Context(), adminURL, migrations.Source, legacy); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(t.Context(), appURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx := tenancy.WithTenant(t.Context(), acme)
	other := tenancy.Tenant{ID: uuid.New(), Slug: "other", Name: "Other"}
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	owner := useOf(uuid.New())
	store := internal.NewLocal(t.TempDir())
	svc := internal.NewService(store, 1<<20, 0, 0)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, id := range ids {
			key := contracts.Key(uuid.NewString())
			if err := store.Put(ctx, contracts.ScopeOfTx(tx), key, strings.NewReader("kept"), 4, contracts.Meta{}); err != nil {
				return err
			}
			if err := tx.DB().Exec(`INSERT INTO files
				(id, tenant_id, name, content_type, size, sha256, storage_key, visibility)
				VALUES (?, ?, 'kept.txt', 'text/plain', 4, ?, ?, 'public')`,
				id, acme.ID, strings.Repeat("a", 64), key.String()).Error; err != nil {
				return err
			}
		}
		return tx.DB().Exec(`INSERT INTO file_uses
			(tenant_id, file_id, module, entity, record, field, locale, public)
			VALUES (?, ?, ?, ?, ?, ?, ?, true)`, acme.ID, ids[0],
			owner.Module, owner.Entity, owner.Record, owner.Field, owner.Locale).Error
	}); err != nil {
		t.Fatal(err)
	}
	// These payloads contradict both local inferences, but belong to another tenant.
	if err := db.Run(tenancy.WithTenant(t.Context(), other), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for i, visibility := range []string{contracts.VisibilityPublic, contracts.VisibilityPrivate} {
			if err := events.Publish(ctx, tx, contracts.EventUploaded, contracts.Uploaded{
				FileID: ids[i], Visibility: visibility, At: db.Now(),
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context(), adminURL, migrations.Source, file.Migrations); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for i, want := range []string{contracts.VisibilityPrivate, contracts.VisibilityPublic} {
			row, err := crud.Get[*contracts.File](tx, ids[i])
			if err != nil {
				return err
			}
			if row.UploadVisibility != want {
				t.Errorf("file %s recovered %q, want %q from its own tenant's evidence", ids[i], row.UploadVisibility, want)
			}
		}
		return svc.SetShown(ctx, tx, owner, []uuid.UUID{ids[0]}, false)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for i, id := range ids {
			_, body, err := svc.Open(ctx, tx, id, true)
			if body != nil {
				body.Close()
			}
			if i == 0 && !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("unpublished promoted file is readable: %v, want not found", err)
			}
			if i == 1 && err != nil {
				t.Errorf("unused public file lost its read: %v", err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
