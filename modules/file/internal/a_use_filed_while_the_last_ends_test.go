package internal_test

import (
	"bytes"
	"context"
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

// The two lock strengths SetUses takes have to conflict with each other, or a
// record that starts showing a picture in the same instant another stops showing
// it can leave the clock running on an image something reads. Each order is held
// open before commit so the other command is seen to wait on it, and the
// assertion is what the real sweep does afterwards: the image stays, charged.

func TestAUseFiledWhileTheLastOneEndsStopsTheReleaseClock(t *testing.T) {
	useFiledAgainstEndingUse(t, true)
}

func TestTheLastUseEndedAfterANewOneIsFiledStartsNoClock(t *testing.T) {
	useFiledAgainstEndingUse(t, false)
}

func useFiledAgainstEndingUse(t *testing.T, endFirst bool) {
	t.Helper()
	admin, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), 1<<20, 0, 0)
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 15*time.Second)
	defer cancel()
	var picture bytes.Buffer
	if err := jpeg.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 20, 10)), nil); err != nil {
		t.Fatal(err)
	}
	old, fresh := useOf(uuid.New()), useOf(uuid.New())
	var id uuid.UUID
	var size int64
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Upload(ctx, held(tx), contracts.Upload{
			Name: "shared.jpg", ContentType: "image/jpeg", Image: true,
			Body: bytes.NewReader(picture.Bytes()), Declared: int64(picture.Len()),
		})
		if err != nil {
			return err
		}
		id, size = row.ID, row.Size
		_, err = svc.SetUses(ctx, tx, old, []uuid.UUID{id})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	ending := func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.SetUses(ctx, tx, old, nil)
		return err
	}
	filing := func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.SetUses(ctx, tx, fresh, []uuid.UUID{id})
		return err
	}
	heldOpen, follower := ending, filing
	if !endFirst {
		heldOpen, follower = filing, ending
	}

	// The first command is held open before commit; the second either finishes
	// against the committed state or is seen waiting on the first's backend.
	ready := make(chan int, 1)
	release := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := heldOpen(ctx, tx); err != nil {
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
		t.Fatalf("first command: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	second := make(chan error, 1)
	go func() {
		second <- db.Run(ctx, conn, follower)
	}()
	finished, waiting := false, false
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
		if len(rows) != 1 || rows[0].Record != fresh.Record {
			t.Fatalf("one record still shows the image, but the ledger says %d uses: %+v", len(rows), rows)
		}
		row, err := crud.Get[*contracts.File](tx, id)
		if err != nil {
			return err
		}
		if row.UnusedSince != nil {
			t.Errorf("a record shows the image, but unused_since = %v; the release sweep's clock is running on a picture something reads", *row.UnusedSince)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sweep := internal.NewUnused(sweepWalk{walking: []tenancy.Tenant{acme}}, time.Nanosecond, 0)
	if err := sweep.Jobs()[0].Run(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := crud.Get[*contracts.File](tx, id); err != nil {
			t.Errorf("the image a record shows, after a sweep: %v, want it kept", err)
		}
		var charged int64
		if err := tx.DB().Model(&contracts.File{}).Select("COALESCE(SUM(size), 0)").Scan(&charged).Error; err != nil {
			return err
		}
		if charged != size {
			t.Errorf("the image a record shows charges %d bytes after the sweep, want %d", charged, size)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
