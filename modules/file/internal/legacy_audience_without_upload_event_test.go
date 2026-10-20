package internal_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// An installation older than the outbox's retention has no file.uploaded event left
// for its files. A row the earlier release promoted (visibility 'public' because a
// published page showed it) must still lose its anonymous read when that page is
// unpublished, as it did before the upgrade; a public file nothing uses (a logo named
// by a bare uuid) keeps its read, as it did before the upgrade.
func TestUnpublishingAPromotedFileWhoseUploadEventIsGoneRevokesItsAudience(t *testing.T) {
	adminURL, appURL := dbtest.URLs(t)
	legacyFiles := fstest.MapFS{}
	for _, name := range []string{
		"000019_file.up.sql", "000040_file_retention.up.sql",
		"000044_file_image_dimensions.up.sql", "000045_file_uses.up.sql",
		"000054_file_variants.up.sql", "000055_file_unused_since.up.sql",
		"000056_file_use_audience.up.sql", "000057_file_unused_since_index.up.sql",
	} {
		data, err := fs.ReadFile(file.Migrations.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		legacyFiles[name] = &fstest.MapFile{Data: data}
	}
	legacy := file.Migrations
	legacy.Files = legacyFiles
	if err := db.Migrate(t.Context(), adminURL, migrations.Source, legacy); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(t.Context(), appURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx := tenancy.WithTenant(t.Context(), acme)
	store := internal.NewLocal(t.TempDir())
	svc := internal.NewService(store, 1<<20, 0, 0)
	owner := useOf(uuid.New())
	promoted, logo := uuid.New(), uuid.New()
	var frame bytes.Buffer
	if err := png.Encode(&frame, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	picture := frame.Bytes()
	digest := fmt.Sprintf("%x", sha256.Sum256(picture))
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, id := range []uuid.UUID{promoted, logo} {
			key := contracts.Key(uuid.NewString())
			if err := store.Put(ctx, contracts.ScopeOfTx(tx), key, bytes.NewReader(picture), int64(len(picture)), contracts.Meta{}); err != nil {
				return err
			}
			if err := tx.DB().Exec(`INSERT INTO files
				(id, tenant_id, name, content_type, size, sha256, storage_key, visibility, width, height)
				VALUES (?, ?, 'legacy.png', 'image/png', ?, ?, ?, 'public', 2, 3)`,
				id, acme.ID, len(picture), digest, key.String()).Error; err != nil {
				return err
			}
		}
		// Only the promoted file is shown by a published record; no upload event
		// survives for either file.
		return tx.DB().Exec(`INSERT INTO file_uses
			(tenant_id, file_id, module, entity, record, field, locale, public)
			VALUES (?, ?, ?, ?, ?, ?, ?, true)`, acme.ID, promoted,
			owner.Module, owner.Entity, owner.Record, owner.Field, owner.Locale).Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context(), adminURL, migrations.Source, file.Migrations); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.SetShown(ctx, tx, owner, []uuid.UUID{promoted}, false)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, body, err := svc.Open(ctx, tx, promoted, true)
		if body != nil {
			body.Close()
		}
		if !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("a file public only because a published record showed it is still served anonymously after unpublish: %v, want not found", err)
		}
		_, body, err = svc.Open(ctx, tx, logo, true)
		if body != nil {
			body.Close()
		}
		if err != nil {
			t.Errorf("a public file nothing uses lost its anonymous read in the upgrade: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
