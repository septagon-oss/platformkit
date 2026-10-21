package internal_test

import (
	"context"
	"errors"
	"strings"
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

func TestDeletionWaitsForACommittedUseAndRefuses(t *testing.T) {
	admin, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), 1<<20, 0, 0)
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 20*time.Second)
	defer cancel()
	var row *contracts.File
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		row, err = svc.Upload(ctx, held(tx), contracts.Upload{
			Name: "shared.txt", ContentType: "text/plain", Declared: -1, Body: strings.NewReader("keep"),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := admin.QueryRowContext(ctx, "SELECT count(*) FROM platformkit_outbox").Scan(&before); err != nil {
		t.Fatal(err)
	}
	ready, release := make(chan int, 1), make(chan struct{})
	filing := make(chan error, 1)
	go func() {
		filing <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			use := contracts.Use{Module: "content", Entity: "content", Record: uuid.New(), Field: "body", Locale: "en"}
			if _, err := svc.SetUses(ctx, tx, use, []uuid.UUID{row.ID}); err != nil {
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
	case err := <-filing:
		t.Fatalf("filing a use: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var returned *contracts.File
	var refusal error
	deleting := make(chan error, 1)
	go func() {
		deleting <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			returned, refusal = svc.Delete(ctx, tx, row.ID)
			return nil
		})
	}()
	finished, waiting := false, false
	var deletionErr error
	for !finished && !waiting {
		select {
		case deletionErr = <-deleting:
			finished = true
		case <-time.After(5 * time.Millisecond):
			if err := admin.QueryRowContext(ctx,
				"SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))", holder).Scan(&waiting); err != nil {
				close(release)
				t.Fatal(err)
			}
		case <-ctx.Done():
			close(release)
			t.Fatal(ctx.Err())
		}
	}
	close(release)
	if err := <-filing; err != nil {
		t.Fatal(err)
	}
	if !finished {
		deletionErr = <-deleting
	}
	if deletionErr != nil {
		t.Fatalf("deletion transaction: %v", deletionErr)
	}
	if returned != nil || !errors.Is(refusal, contracts.ErrInUse) {
		t.Errorf("deletion after a use committed returned row=%v error=%v; want nil row and ErrInUse", returned != nil, refusal)
	}
	var files, uses, after int
	if err := admin.QueryRowContext(ctx, "SELECT count(*) FROM files WHERE id=$1", row.ID).Scan(&files); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(ctx, "SELECT count(*) FROM file_uses WHERE file_id=$1", row.ID).Scan(&uses); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(ctx, "SELECT count(*) FROM platformkit_outbox").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if files != 1 || uses != 1 || after != before {
		t.Errorf("deletion left files=%d uses=%d outbox=%d->%d; want the used file and unchanged outbox", files, uses, before, after)
	}
}
