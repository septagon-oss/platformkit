package internal_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

func TestConcurrentLastUsesStartTheReleaseClock(t *testing.T) {
	concurrentLastUses(t, false)
}

func TestConcurrentPublicUsesRetractTheAudience(t *testing.T) {
	concurrentLastUses(t, true)
}

func concurrentLastUses(t *testing.T, public bool) {
	t.Helper()
	admin, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), 1<<20, 0, 0)
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 15*time.Second)
	defer cancel()
	var picture bytes.Buffer
	if err := jpeg.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 20, 10)), nil); err != nil {
		t.Fatal(err)
	}
	uses := []contracts.Use{useOf(uuid.New()), useOf(uuid.New())}
	var id uuid.UUID
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Upload(ctx, held(tx), contracts.Upload{
			Name: "shared.jpg", ContentType: "image/jpeg", Image: true,
			Body: bytes.NewReader(picture.Bytes()), Declared: int64(picture.Len()),
		})
		if err != nil {
			return err
		}
		id = row.ID
		for _, use := range uses {
			if _, err := svc.SetUses(ctx, tx, use, []uuid.UUID{id}); err != nil {
				return err
			}
			if public {
				if err := svc.SetShown(ctx, tx, use, []uuid.UUID{id}, true); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Hold the first successful removal before commit. The second either finishes
	// against that snapshot or waits for its lock; both are valid ways to reach
	// the assertion. This does not require the missing lock to remain missing.
	ready := make(chan int, 1)
	release := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if _, err := svc.SetUses(ctx, tx, uses[0], nil); err != nil {
				return err
			}
			var pid int
			if err := tx.DB().Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
				return err
			}
			ready <- pid
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	var holder int
	select {
	case holder = <-ready:
	case err := <-first:
		t.Fatalf("first removal: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	second := make(chan error, 1)
	go func() {
		second <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.SetUses(ctx, tx, uses[1], nil)
			return err
		})
	}()
	finished := false
	waiting := false
	for !finished && !waiting {
		select {
		case err := <-second:
			if err != nil {
				t.Fatal(err)
			}
			finished = true
		case <-time.After(5 * time.Millisecond):
			if err := admin.QueryRowContext(ctx,
				"SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))", holder).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if !finished {
		if err := <-second; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, err := svc.Uses(ctx, tx, id)
		if err != nil {
			return err
		}
		if len(rows) != 0 {
			t.Fatalf("both records dropped the image, but %d uses remain", len(rows))
		}
		row, err := crud.Get[*contracts.File](tx, id)
		if err != nil {
			return err
		}
		if row.UnusedSince == nil {
			t.Error("both removals committed and no uses remain, but unused_since is NULL; the release sweep will never select this image")
		}
		if public && row.Visibility != contracts.VisibilityPrivate {
			t.Errorf("last public uses ended, visibility = %s, want private", row.Visibility)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A short configured grace exercises the real sweep without sleeping or
	// inventing a timestamp for a row whose command failed to record one.
	sweep := internal.NewUnused(sweepWalk{walking: []tenancy.Tenant{acme}}, time.Nanosecond, 0)
	if err := sweep.Jobs()[0].Run(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := crud.Get[*contracts.File](tx, id); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("unused image after its grace and a sweep: %v, want not found", err)
		}
		var charged int64
		if err := tx.DB().Model(&contracts.File{}).Select("COALESCE(SUM(size), 0)").Scan(&charged).Error; err != nil {
			return err
		}
		if charged != 0 {
			t.Errorf("unused image still charges %d bytes after the sweep", charged)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
