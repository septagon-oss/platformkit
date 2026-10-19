package internal_test

import (
	"context"
	"errors"
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

// Delete and a save that begins a use of the same file are one decision: each
// order is held open before commit while the other runs, and afterwards either
// the file is gone and nothing names it, or the file stays and the use is
// counted. A file removed under a fresh use, or a use filed for a removed file,
// is the interleave the delete's lock exists to forbid.

func TestADeleteHeldOpenRefusesTheSaveThatNamesItsFile(t *testing.T) {
	deleteAgainstFiledUse(t, true)
}

func TestASaveHeldOpenIsCountedByTheDeleteThatFollows(t *testing.T) {
	deleteAgainstFiledUse(t, false)
}

func deleteAgainstFiledUse(t *testing.T, deleteFirst bool) {
	t.Helper()
	admin, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), 1<<20, 0, 0)
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 15*time.Second)
	defer cancel()
	var id uuid.UUID
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Upload(ctx, held(tx), uploaded("shown.png"))
		if err != nil {
			return err
		}
		id = row.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	page := useOf(uuid.New())
	var deleteErr, saveErr error
	removing := func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, deleteErr = svc.Delete(ctx, tx, id)
		return deleteErr
	}
	filing := func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, saveErr = svc.SetUses(ctx, tx, page, []uuid.UUID{id})
		return saveErr
	}
	heldOpen, follower := removing, filing
	if !deleteFirst {
		heldOpen, follower = filing, removing
	}

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
		t.Fatalf("the command held open: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	second := make(chan error, 1)
	go func() {
		second <- db.Run(ctx, conn, follower)
	}()
	finished, waiting := false, false
	var secondErr error
	for !finished && !waiting {
		select {
		case secondErr = <-second:
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
		t.Fatalf("the command held open: %v", err)
	}
	if !finished {
		secondErr = <-second
	}

	var uses, rows int
	if err := admin.QueryRowContext(ctx, "SELECT count(*) FROM file_uses WHERE file_id = $1", id).Scan(&uses); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(ctx, "SELECT count(*) FROM files WHERE id = $1", id).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if deleteFirst {
		if deleteErr != nil {
			t.Fatalf("delete of a file nothing showed yet = %v", deleteErr)
		}
		if !errors.Is(saveErr, crud.ErrNotFound) || secondErr == nil {
			t.Errorf("a save naming a file removed under it = %v, want refused as not found", saveErr)
		}
		if rows != 0 || uses != 0 {
			t.Errorf("after the delete won: %d file rows and %d uses, want none of either", rows, uses)
		}
		return
	}
	if saveErr != nil {
		t.Fatalf("the save held open = %v", saveErr)
	}
	var inUse *contracts.InUse
	if !errors.As(deleteErr, &inUse) || inUse.Records != 1 {
		t.Errorf("a delete after a save that began a use = %v, want ErrInUse naming one record", deleteErr)
	}
	if rows != 1 || uses != 1 {
		t.Errorf("after the save won: %d file rows and %d uses, want the file and its one use", rows, uses)
	}
}
