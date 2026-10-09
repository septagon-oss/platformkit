package internal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// TestTwoEditsOfOneRevisionLetOnlyTheFirstWrite: two editors save copies of the
// same revision at the same moment. The second waits on the row's lock, and when
// the first commits it must find the row moved on — the revision is compared
// after the lock is taken, not before — so it writes nothing, publishes nothing
// and returns no row, and the first editor's name is what is stored.
func TestTwoEditsOfOneRevisionLetOnlyTheFirstWrite(t *testing.T) {
	admin, conn := dbtest.Schema(t, notification.Migrations)
	ctx, cancel := context.WithTimeout(asAdmin(tenancy.WithTenant(t.Context(), acme)), 10*time.Second)
	defer cancel()
	store := senders()
	var read contracts.Sender
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := store.Put(ctx, tx, sender())
		if err == nil {
			read = *row
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	firstSaved, releaseFirst := make(chan struct{}), make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			edit := read
			edit.FromName = "First editor"
			if _, err := store.Put(ctx, tx, edit); err != nil {
				return err
			}
			close(firstSaved)
			select {
			case <-releaseFirst:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-firstSaved:
	case err := <-firstDone:
		t.Fatalf("first edit: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	type answer struct {
		row *contracts.Sender
		err error
	}
	backend := make(chan int, 1)
	second := make(chan answer, 1)
	go func() {
		var a answer
		err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var pid int
			if err := tx.DB().Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
				return err
			}
			backend <- pid
			edit := read
			edit.FromName = "Second editor"
			a.row, a.err = store.Put(ctx, tx, edit)
			return nil
		})
		if a.err == nil {
			a.err = err
		}
		second <- a
	}()
	var pid int
	select {
	case pid = <-backend:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for waiting := false; !waiting; {
		if err := admin.QueryRowContext(ctx, "SELECT COALESCE(wait_event_type = 'Lock', false) FROM pg_stat_activity WHERE pid = $1", pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ticker.C:
		case a := <-second:
			t.Fatalf("second edit finished while the first held the row: err=%v row=%v", a.err, a.row != nil)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first edit: %v", err)
	}

	var a answer
	select {
	case a = <-second:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if !errors.Is(a.err, crud.ErrConflict) || a.row != nil {
		t.Errorf("second edit of revision %d: error=%v, returned row=%v; want conflict and no row", read.Revision, a.err, a.row != nil)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		saved, err := store.For(ctx, tx)
		if err != nil {
			return err
		}
		if saved == nil || saved.FromName != "First editor" || saved.Revision != read.Revision+1 {
			t.Errorf("stored sender = %+v; want the first editor's name at revision %d", saved, read.Revision+1)
		}
		// One for the first save and one for the first editor: the refused edit
		// published nothing.
		var set int64
		if err := tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventSenderSet).Count(&set).Error; err != nil {
			return err
		}
		if set != 2 {
			t.Errorf("%d %s events, want 2 (the first save and the first editor's)", set, contracts.EventSenderSet)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
