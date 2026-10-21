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
	"github.com/septagon-oss/platformkit/kit/push"
	pushlog "github.com/septagon-oss/platformkit/kit/push/providers/log"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
)

// A removal that waits behind another removal of the same device, and one that
// arrives after it, both find nothing: the device is deleted once and
// notification.device_removed is published once.
func TestTwoRemovalsOfOneDeviceDeleteItOnceAndPublishOnce(t *testing.T) {
	admin, conn := dbtest.Schema(t, notification.Migrations)
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 20*time.Second)
	defer cancel()
	owner := tenancy.WithPrincipal(tenancy.WithActor(ctx, notificationtest.Ada), tenancy.Principal{UserID: notificationtest.Ada})
	devices := notification.Devices(pushlog.NewHub().Adapters())
	var id uuid.UUID
	if err := db.Run(owner, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := devices.RegisterDevice(ctx, tx, notificationtest.Ada,
			contracts.DeviceInput{Platform: push.PlatformExpo, Token: "ExponentPushToken[removed-from-two-tabs]"})
		if err == nil {
			id = row.ID
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	waiting := make(chan error, 1)
	if err := db.Run(owner, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := devices.RemoveDevice(ctx, tx, id, notificationtest.Ada); err != nil {
			return err
		}
		backend := make(chan int, 1)
		go func() {
			waiting <- db.Run(owner, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				var pid int
				if err := tx.DB().Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
					return err
				}
				backend <- pid
				return devices.RemoveDevice(ctx, tx, id, notificationtest.Ada)
			})
		}()
		waitForDeviceCommandLock(t, ctx, admin, backend, waiting)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-waiting:
		if !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("removal that waited behind the first: %v; want ErrNotFound", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := db.Run(owner, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := devices.RemoveDevice(ctx, tx, id, notificationtest.Ada); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("removal after the first committed: %v; want ErrNotFound", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(owner, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, count, err := devices.ListDevices(ctx, tx, notificationtest.Ada, crud.Query{})
		if err != nil || count != 0 {
			t.Errorf("devices after removal: %d (%v); want none", count, err)
		}
		removals := 0
		for _, name := range outbox(t, tx) {
			if name == contracts.EventDeviceRemoved {
				removals++
			}
		}
		if removals != 1 {
			t.Errorf("published %d device_removed events for one removed device; want 1", removals)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
