package internal_test

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

func TestPublishedRecordsWithTwoImageFieldsSaveTogether(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), 1<<20, 0, 0)
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 20*time.Second)
	defer cancel()
	var picture bytes.Buffer
	if err := jpeg.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 20, 10)), nil); err != nil {
		t.Fatal(err)
	}
	var ids [2]uuid.UUID
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for i := range ids {
			row, err := svc.Upload(ctx, held(tx), contracts.Upload{
				Name: "picture.jpg", ContentType: "image/jpeg", Image: true,
				Body: bytes.NewReader(picture.Bytes()), Declared: int64(picture.Len()),
			})
			if err != nil {
				return err
			}
			ids[i] = row.ID
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	ready := make(chan error, 2)
	continueSave := make(chan struct{})
	done := make(chan error, 2)
	for i := range 2 {
		go func() {
			done <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				use := useOf(uuid.New())
				use.Field = "body"
				_, err := svc.SetUsesShown(ctx, tx, use, []uuid.UUID{ids[i]}, true)
				ready <- err
				if err != nil {
					return err
				}
				select {
				case <-continueSave:
				case <-ctx.Done():
					return ctx.Err()
				}
				use.Field = "summary"
				_, err = svc.SetUsesShown(ctx, tx, use, []uuid.UUID{ids[1-i]}, true)
				return err
			})
		}()
	}
	for range 2 {
		if err := <-ready; err != nil {
			close(continueSave)
			t.Fatal(err)
		}
	}
	close(continueSave)
	for range 2 {
		if err := <-done; err != nil {
			t.Errorf("saving a published record with two image fields: %v; both independent records must save", err)
		}
	}
}
