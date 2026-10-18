package internal_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/push"
	pushlog "github.com/septagon-oss/platformkit/kit/push/providers/log"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
)

func TestDeviceRemovalRechecksOwnershipAfterAConcurrentTransfer(t *testing.T) {
	admin, conn := dbtest.Schema(t, notification.Migrations)
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 20*time.Second)
	defer cancel()
	owner := tenancy.WithPrincipal(tenancy.WithActor(ctx, notificationtest.Ada), tenancy.Principal{UserID: notificationtest.Ada})
	buyer := tenancy.WithPrincipal(tenancy.WithActor(ctx, notificationtest.Bob), tenancy.Principal{UserID: notificationtest.Bob})
	devices := notification.Devices(pushlog.NewHub().Adapters())
	in := contracts.DeviceInput{Platform: push.PlatformExpo, Token: "ExponentPushToken[transferred-phone]"}
	var id uuid.UUID
	if err := db.Run(owner, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := devices.RegisterDevice(ctx, tx, notificationtest.Ada, in)
		if err == nil {
			id = row.ID
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	removed := make(chan error, 1)
	if err := db.Run(buyer, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := crud.GetForUpdate[*contracts.Device](tx, id); err != nil {
			return err
		}
		backend := make(chan int, 1)
		go func() {
			removed <- db.Run(owner, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				var pid int
				if err := tx.DB().Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
					return err
				}
				backend <- pid
				return devices.RemoveDevice(ctx, tx, id, notificationtest.Ada)
			})
		}()
		waitForDeviceCommandLock(t, ctx, admin, backend, removed)
		row, err := devices.RegisterDevice(ctx, tx, notificationtest.Bob, in)
		if err == nil && (row.ID != id || row.RecipientID != notificationtest.Bob) {
			t.Error("transfer did not retain the row and name its new owner")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-removed:
		if !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("previous owner removed the transferred device: %v; want ErrNotFound", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := db.Run(buyer, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		row, err := crud.Get[*contracts.Device](tx, id)
		if err != nil || row.RecipientID != notificationtest.Bob {
			t.Errorf("new owner's device was lost: row=%v error=%v", row != nil, err)
		}
		for _, name := range outbox(t, tx) {
			if name == contracts.EventDeviceRemoved {
				t.Error("refused previous-owner removal published device_removed")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// waitForDeviceCommandLock observes the database wait, whether the command locks
// before checking ownership or only when it writes. It assumes neither answer.
func waitForDeviceCommandLock(t *testing.T, ctx context.Context, admin *sql.DB, backend <-chan int, done <-chan error) {
	t.Helper()
	var pid int
	select {
	case pid = <-backend:
	case err := <-done:
		t.Fatalf("command did not start its transaction: %v", err)
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
			return
		}
		select {
		case <-ticker.C:
		case err := <-done:
			t.Fatalf("command finished while its row or index entry was held: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
