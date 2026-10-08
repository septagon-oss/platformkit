package internal_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

type heldVerification struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (v *heldVerification) Verify(ctx context.Context, _ contracts.Sender) (string, error) {
	if v.calls.Add(1) == 1 {
		close(v.entered)
	}
	select {
	case <-v.release:
		return "domain proof", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestConcurrentSenderVerificationChecksAndPublishesOnce(t *testing.T) {
	admin, conn := dbtest.Schema(t, notification.Migrations)
	ctx, cancel := context.WithTimeout(asAdmin(tenancy.WithTenant(t.Context(), acme)), 10*time.Second)
	defer cancel()
	verifier := &heldVerification{entered: make(chan struct{}), release: make(chan struct{})}
	defer func() {
		select {
		case <-verifier.release:
		default:
			close(verifier.release)
		}
	}()
	store := &internal.Senders{Verifier: verifier}
	var saved *contracts.Sender
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		saved, err = store.Put(ctx, tx, sender())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	verify := func(backend chan int) {
		done <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if backend != nil {
				var pid int
				if err := tx.DB().Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
					return err
				}
				backend <- pid
			}
			row, err := store.Verify(ctx, tx, saved.ID)
			if err == nil && (row == nil || row.Status != contracts.SenderVerified) {
				return fmt.Errorf("verification returned no verified row")
			}
			return err
		})
	}
	go verify(nil)
	select {
	case <-verifier.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	backend := make(chan int, 1)
	go verify(backend)
	var pid int
	select {
	case pid = <-backend:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := admin.QueryRowContext(ctx, "SELECT COALESCE(wait_event_type = 'Lock', false) FROM pg_stat_activity WHERE pid = $1", pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if verifier.calls.Load() > 1 {
			t.Fatal("concurrent verification reached the verifier before the first transaction committed")
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	close(verifier.release)
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if got := verifier.calls.Load(); got != 1 {
		t.Errorf("verifier called %d times, want once", got)
	}
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var count int64
		if err := tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventSenderVerified).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			t.Errorf("concurrent verification published %d events, want one", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
