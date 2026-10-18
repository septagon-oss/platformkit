package internal_test

import (
	"context"
	"testing"
	"time"

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

func TestConcurrentDeviceRegistrationsReturnTheCommittedDevice(t *testing.T) {
	admin, conn := dbtest.Schema(t, notification.Migrations)
	ctx, cancel := context.WithTimeout(tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), acme),
		tenancy.Principal{UserID: notificationtest.Ada}), 20*time.Second)
	defer cancel()
	devices := notification.Devices(pushlog.NewHub().Adapters())
	in := contracts.DeviceInput{Platform: push.PlatformExpo, Token: "ExponentPushToken[one-device]"}
	var first, second *contracts.Device
	done := make(chan error, 1)
	if err := db.Run(ctx, conn, func(transactionCtx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		first, err = devices.RegisterDevice(transactionCtx, tx, notificationtest.Ada, in)
		if err != nil {
			return err
		}
		backend := make(chan int, 1)
		go func() {
			done <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				var pid int
				if err := tx.DB().Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
					return err
				}
				backend <- pid
				var err error
				second, err = devices.RegisterDevice(ctx, tx, notificationtest.Ada, in)
				return err
			})
		}()
		waitForDeviceCommandLock(t, ctx, admin, backend, done)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil || second == nil {
			t.Errorf("retry of the same registration: row=%v error=%v; want the committed device", second != nil, err)
		} else if second.ID != first.ID {
			t.Errorf("one token has two identities: %s and %s", first.ID, second.ID)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		count, err := crud.Count[*contracts.Device](tx)
		if err != nil {
			return err
		}
		if count != 1 || len(outbox(t, tx)) != 1 {
			t.Errorf("concurrent registration committed %d rows and %d events; want one each", count, len(outbox(t, tx)))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
