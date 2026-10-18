package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

func TestUnpublishingPreservesTheUploadAudienceAndRollsBackItsEvents(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), 1<<20, 0, 0)
	ctx := tenancy.WithTenant(t.Context(), acme)
	owner := useOf(uuid.New())
	var ids []uuid.UUID
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, visibility := range []string{contracts.VisibilityPublic, contracts.VisibilityPrivate} {
			up := uploaded("shown.png")
			up.Visibility = visibility
			row, err := svc.Upload(ctx, held(tx), up)
			if err != nil {
				return err
			}
			ids = append(ids, row.ID)
		}
		if _, err := svc.SetUses(ctx, tx, owner, ids); err != nil {
			return err
		}
		return svc.SetShown(ctx, tx, owner, ids, true)
	}); err != nil {
		t.Fatal(err)
	}

	check := func(shown bool, flips int64) {
		t.Helper()
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			for i, id := range ids {
				row, err := crud.Get[*contracts.File](tx, id)
				if err != nil {
					return err
				}
				want := contracts.VisibilityPrivate
				if i == 0 {
					want = contracts.VisibilityPublic
				}
				if row.Visibility != want {
					t.Errorf("upload audience = %s, want %s", row.Visibility, want)
				}
				_, body, err := svc.Open(ctx, tx, id, true)
				if body != nil {
					body.Close()
				}
				if i == 0 || shown {
					if err != nil {
						t.Errorf("publicly readable image %s: %v", id, err)
					}
				} else if !errors.Is(err, crud.ErrNotFound) {
					t.Errorf("unpublished private upload is anonymously readable: %v", err)
				}
				var uses []contracts.FileUse
				if err := tx.DB().Where("file_id = ?", id).Find(&uses).Error; err != nil {
					return err
				}
				if len(uses) != 1 || uses[0].Public != shown {
					t.Errorf("uses = %+v, want one use with public %t", uses, shown)
				}
			}
			var count int64
			if err := tx.DB().Table(outbox).Where("name = ?", contracts.EventVisibility).Count(&count).Error; err != nil {
				return err
			}
			if count != flips {
				t.Errorf("audience events = %d, want %d", count, flips)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	check(true, 1)
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := svc.SetShown(ctx, tx, owner, ids, false); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("rolled-back unpublish: %v", err)
	}
	check(true, 1)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.SetShown(ctx, tx, owner, ids, false)
	}); err != nil {
		t.Fatal(err)
	}
	check(false, 2)
}
