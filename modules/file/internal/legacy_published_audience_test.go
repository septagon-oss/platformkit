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
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

func TestUnpublishingALegacyRecordRevokesItsPrivateUploadsAudience(t *testing.T) {
	checkLegacyAudienceRevocation(t, false)
}

func TestEndingALegacyRecordsLastUseRevokesItsPrivateUploadsAudience(t *testing.T) {
	checkLegacyAudienceRevocation(t, true)
}

func checkLegacyAudienceRevocation(t *testing.T, ending bool) {
	t.Helper()
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
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	var frame bytes.Buffer
	if err := png.Encode(&frame, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	picture := frame.Bytes()
	digest := fmt.Sprintf("%x", sha256.Sum256(picture))
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for i, id := range ids {
			original := contracts.VisibilityPrivate
			if i == 1 {
				original = contracts.VisibilityPublic
			}
			key := contracts.Key(uuid.NewString())
			if err := store.Put(ctx, contracts.ScopeOfTx(tx), key, bytes.NewReader(picture), int64(len(picture)), contracts.Meta{}); err != nil {
				return err
			}
			// Earlier publishing stored its derived public answer in files.visibility,
			// replacing the private choice recorded by the upload event.
			if err := tx.DB().Exec(`INSERT INTO files
				(id, tenant_id, name, content_type, size, sha256, storage_key, visibility, width, height)
				VALUES (?, ?, 'legacy.png', 'image/png', ?, ?, ?, 'public', 2, 3)`,
				id, acme.ID, len(picture), digest, key.String()).Error; err != nil {
				return err
			}
			if err := tx.DB().Exec(`INSERT INTO file_uses
				(tenant_id, file_id, module, entity, record, field, locale, public)
				VALUES (?, ?, ?, ?, ?, ?, ?, true)`, acme.ID, id,
				owner.Module, owner.Entity, owner.Record, owner.Field, owner.Locale).Error; err != nil {
				return err
			}
			if err := events.Publish(ctx, tx, contracts.EventUploaded, contracts.Uploaded{
				FileID: id, Name: "legacy.png", ContentType: "image/png", Size: int64(len(picture)),
				SHA256: digest, Visibility: original, At: db.Now(),
			}); err != nil {
				return err
			}
			if i == 0 {
				if err := events.Publish(ctx, tx, contracts.EventVisibility, contracts.Visibility{
					FileID: id, Visibility: contracts.VisibilityPublic, Module: owner.Module,
					Entity: owner.Entity, Record: owner.Record, At: db.Now(),
				}); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context(), adminURL, migrations.Source, file.Migrations); err != nil {
		t.Fatalf("upgrade the published files: %v", err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, id := range ids {
			_, body, err := svc.Open(ctx, tx, id, true)
			if err != nil {
				return err
			}
			body.Close()
		}
		return nil
	}); err != nil {
		t.Fatalf("the published images before revocation: %v", err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if ending {
			_, err := svc.SetUses(ctx, tx, owner, nil)
			return err
		}
		return svc.SetShown(ctx, tx, owner, ids, false)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var publicUses int64
		if err := tx.DB().Model(&contracts.FileUse{}).Where("file_id IN ? AND public", ids).Count(&publicUses).Error; err != nil {
			return err
		}
		if publicUses != 0 {
			t.Errorf("audience revocation left %d public uses, want none", publicUses)
		}
		for i, id := range ids {
			_, body, err := svc.Open(ctx, tx, id, true)
			if body != nil {
				body.Close()
			}
			if i == 0 && !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("a private upload promoted by the legacy published record is still served anonymously after audience revocation: %v, want not found", err)
			}
			if i == 1 && err != nil {
				t.Errorf("an upload originally given public lost its audience: %v", err)
			}
		}
		var revocations int64
		if err := tx.DB().Table(outbox).Where("name = ? AND payload->>'fileId' = ? AND payload->>'visibility' = ?",
			contracts.EventVisibility, ids[0].String(), contracts.VisibilityPrivate).Count(&revocations).Error; err != nil {
			return err
		}
		if revocations != 1 {
			t.Errorf("private-upload audience revocations = %d, want one", revocations)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
