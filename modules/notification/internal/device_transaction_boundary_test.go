package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/push"
	pushlog "github.com/septagon-oss/platformkit/kit/push/providers/log"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

func TestDeviceReadsAndWritesStayWithinTheTenant(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	devices := notification.Devices(pushlog.NewHub().Adapters())
	person := tenancy.WithPrincipal(t.Context(), tenancy.Principal{UserID: notificationtest.Ada})
	in := contracts.DeviceInput{Platform: push.PlatformExpo, Token: "ExponentPushToken[same-device-two-tenants]"}
	var first uuid.UUID
	if err := db.Run(tenancy.WithTenant(person, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := devices.RegisterDevice(ctx, tx, notificationtest.Ada, in)
		if err == nil {
			first = row.ID
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(tenancy.WithTenant(person, globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, count, err := devices.ListDevices(ctx, tx, notificationtest.Ada, crud.Query{})
		if err != nil || count != 0 || len(rows) != 0 {
			t.Errorf("other tenant read %d devices (%v); want none", count, err)
		}
		if row, err := crud.Get[*contracts.Device](tx, first); row != nil || !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("other tenant read device by id: row=%v error=%v", row != nil, err)
		}
		if err := devices.RemoveDevice(ctx, tx, first, notificationtest.Ada); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("other tenant removed device: %v", err)
		}
		if len(outbox(t, tx)) != 0 {
			t.Error("foreign removal emitted an event")
		}
		row, err := devices.RegisterDevice(ctx, tx, notificationtest.Ada, in)
		if err == nil && (row.ID == first || row.TenantID != globex.ID) {
			t.Error("foreign registration moved the first tenant's device")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(tenancy.WithTenant(person, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, count, err := devices.ListDevices(ctx, tx, notificationtest.Ada, crud.Query{})
		if err == nil && (count != 1 || len(rows) != 1 || rows[0].ID != first || len(outbox(t, tx)) != 1) {
			t.Error("other tenant changed the original device or its events")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGoneDeviceCleanupAndDeliveryCommitTogetherAndIgnoreReplay(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), acme), tenancy.Principal{UserID: notificationtest.Ada})
	adapters := push.Adapters{push.PlatformExpo: goneService{}}
	svc, manifest := notification.New(notification.Deps{
		Recipients: directory{}, Hosts: hosts{}, Mailer: notification.NewMailbox(), Push: adapters,
	})
	sub := subscriptionNamed(t, contracts.EventPushRequested, manifest.Subscriptions)
	var deviceID, noticeID uuid.UUID
	var event events.Event
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		device, err := notification.Devices(adapters).RegisterDevice(ctx, tx, notificationtest.Ada,
			contracts.DeviceInput{Platform: push.PlatformExpo, Token: "ExponentPushToken[gone-atomically]"})
		if err != nil {
			return err
		}
		deviceID = device.ID
		notice, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "One push", Wants: contracts.WantsPush})
		if err != nil {
			return err
		}
		noticeID = notice.ID
		payload, err := json.Marshal(contracts.DeliveryRequested{NotificationID: noticeID, Recipient: notificationtest.Ada})
		event = events.Event{ID: uuid.New(), Name: sub.Name, Payload: payload}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := sub.Handler(ctx, tx, event); err != nil {
			return err
		}
		return errRollback
	}); !errors.Is(err, errRollback) {
		t.Fatalf("delivery rollback: %v", err)
	}
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := crud.Get[*contracts.Device](tx, deviceID); err != nil {
			t.Errorf("rolled-back cleanup lost the device: %v", err)
		}
		if got := lastLedger(t, tx, noticeID, contracts.ChannelPush); got.Outcome != "requested" {
			t.Errorf("rolled-back delivery closed the channel: %s", got.Outcome)
		}
		for _, name := range outbox(t, tx) {
			if name == contracts.EventDeviceRemoved {
				t.Error("rolled-back cleanup left an event")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var first []internal.Delivery
	var firstEvents []string
	for attempt := range 2 {
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := sub.Handler(ctx, tx, event); err != nil {
				return err
			}
			if _, err := crud.Get[*contracts.Device](tx, deviceID); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("committed gone device remains: %v", err)
			}
			if got := lastLedger(t, tx, noticeID, contracts.ChannelPush); got.Outcome != "failed" {
				t.Errorf("gone device left channel %s, want failed", got.Outcome)
			}
			rows, err := internal.Deliveries(tx, noticeID)
			if err != nil {
				return err
			}
			published := outbox(t, tx)
			if attempt == 0 {
				first, firstEvents = rows, published
			} else if !reflect.DeepEqual(rows, first) || !reflect.DeepEqual(published, firstEvents) {
				t.Error("replayed event changed committed delivery or outbox state")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}
