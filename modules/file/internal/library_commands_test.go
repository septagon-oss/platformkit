package internal_test

import (
	"context"
	"errors"
	"strings"
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

func TestUsedFileDeletionRefusesWithoutReturningARow(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), 1<<20, 0, 0)
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Upload(ctx, held(tx), contracts.Upload{Name: "referenced.txt", ContentType: "text/plain", Declared: -1, Body: strings.NewReader("keep")})
		if err != nil {
			return err
		}
		use := contracts.Use{Module: "content", Entity: "content", Record: uuid.New(), Field: "body", Locale: "en"}
		if _, err := svc.SetUses(ctx, tx, use, []uuid.UUID{row.ID}); err != nil {
			return err
		}
		row, err = crud.Get[*contracts.File](tx, row.ID)
		if err != nil {
			return err
		}
		before := len(published(t, tx))
		returned, err := svc.Delete(ctx, tx, row.ID)
		if !errors.Is(err, contracts.ErrInUse) || returned != nil {
			t.Errorf("delete while used returned row=%v error=%v; want no row and ErrInUse", returned, err)
		}
		if len(published(t, tx)) != before {
			t.Error("a refused deletion emitted an event")
		}
		current, err := crud.Get[*contracts.File](tx, row.ID)
		if err != nil {
			return err
		}
		if !current.UpdatedAt.Equal(row.UpdatedAt) {
			t.Error("a refused deletion changed the row")
		}
		if _, err := svc.SetUses(ctx, tx, use, nil); err != nil {
			return err
		}
		if _, err := svc.Delete(ctx, tx, row.ID); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentCaptionsKeepTheFirstCommit(t *testing.T) {
	admin, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), 1<<20, 0, 0)
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 20*time.Second)
	defer cancel()
	var row *contracts.File
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		row, err = svc.Upload(ctx, held(tx), contracts.Upload{Name: "caption.txt", ContentType: "text/plain", Declared: -1, Body: strings.NewReader("frame")})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ready := make(chan int, 1)
	release := make(chan struct{})
	first, second := make(chan error, 1), make(chan error, 1)
	go func() {
		first <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if _, err := svc.SetAlt(ctx, tx, row.ID, "first caption", false, row.UpdatedAt); err != nil {
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
		t.Fatalf("first caption: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	go func() {
		second <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			returned, err := svc.SetAlt(ctx, tx, row.ID, "second caption", false, row.UpdatedAt)
			if returned != nil {
				return errors.New("refused stale caption returned a row")
			}
			return err
		})
	}()
	waiting := false
	for !waiting {
		select {
		case err := <-second:
			close(release)
			t.Fatalf("second caption did not wait for the row lock: %v", err)
		case <-time.After(5 * time.Millisecond):
			if err := admin.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))", holder).Scan(&waiting); err != nil {
				close(release)
				t.Fatal(err)
			}
		case <-ctx.Done():
			close(release)
			t.Fatal(ctx.Err())
		}
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("second caption: %v, want ErrConflict", err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		current, err := crud.Get[*contracts.File](tx, row.ID)
		if err != nil {
			return err
		}
		if current.Alt != "first caption" {
			t.Errorf("caption=%q; want the first commit", current.Alt)
		}
		var changed int64
		if err := tx.DB().Table(outbox).Where("name = ?", contracts.EventAltered).Count(&changed).Error; err != nil {
			return err
		}
		if changed != 1 {
			t.Errorf("caption events=%d; want one committed change", changed)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
