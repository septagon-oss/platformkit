package internal_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// interruptedImageRead keeps the real store's behavior except for one
// unavailable read, as can occur while an object store restarts.
type interruptedImageRead struct {
	contracts.Storage
	unavailable atomic.Bool
}

func (s *interruptedImageRead) Get(ctx context.Context, scope contracts.Scope, key contracts.Key) (io.ReadCloser, error) {
	if s.unavailable.Swap(false) {
		return nil, errors.New("object store temporarily unavailable")
	}
	return s.Storage.Get(ctx, scope, key)
}

func TestWidthJobRetriesAfterTheObjectStoreRecovers(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	ctx := tenancy.WithTenant(t.Context(), acme)
	store := &interruptedImageRead{Storage: internal.NewLocal(t.TempDir())}
	svc := internal.NewService(store, 16<<20, 0, 0)
	var picture bytes.Buffer
	if err := jpeg.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 700, 350)), nil); err != nil {
		t.Fatal(err)
	}
	var parent *contracts.File
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		parent, err = svc.Upload(ctx, held(tx), contracts.Upload{Name: "wide.jpg", ContentType: "image/jpeg", Body: bytes.NewReader(picture.Bytes()), Declared: int64(picture.Len())})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	job := internal.NewVariants(svc, store, sweepWalk{walking: []tenancy.Tenant{acme}}, time.Second).Jobs()[0]
	store.unavailable.Store(true)
	if err := job.Run(ctx, conn); err == nil {
		t.Error("an unavailable object store must report a failed tick, not successful processing")
	}
	// The next tick uses the healthy real store and the same persisted work.
	if err := job.Run(ctx, conn); err != nil {
		t.Fatalf("tick after recovery: %v", err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var rows []contracts.Variant
		if err := tx.DB().Where("file_id = ?", parent.ID).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].State != contracts.VariantReady {
			t.Errorf("after storage recovery, widths = %+v; want the 480 px width ready", rows)
		}
		_, body, err := svc.OpenWidth(ctx, tx, parent.ID, false, 480)
		if err != nil {
			t.Errorf("width remains unavailable after the store recovered: %v", err)
			return nil
		}
		defer body.Close()
		frame, _, err := image.DecodeConfig(body)
		if err != nil || frame.Width != 480 {
			t.Errorf("recovered width = %+v, %v", frame, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
