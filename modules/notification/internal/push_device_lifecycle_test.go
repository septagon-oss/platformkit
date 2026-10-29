package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/secrets"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
	"github.com/septagon-oss/platformkit/pkit"
)

// goneService is the push provider that has stopped knowing the token: every
// send answers ErrGone, which is the answer that costs the caller a row. The log
// adapter never reports a device gone on purpose, so the gone path needs a
// stand-in that does, named in kit/push/providers/log's own guide.
type goneService struct{}

func (goneService) Platform() push.Platform { return push.PlatformExpo }

func (goneService) Validate(string) error { return nil }

func (goneService) Send(context.Context, push.Device, push.Message) (push.Receipt, error) {
	return push.Receipt{}, push.Gone("expo push ticket: DeviceNotRegistered")
}

// TestAPushNoticeReachesTheCarrierAndAGoneDeviceIsRemoved is the delivery side
// of the device table and the reason the table exists: a notice that asked for
// push reaches the composed adapter carrying the recipient, the tenant and the
// words — and when the provider says the token is not a device any more, the row
// goes in the same transaction as the ledger row that closes the channel, with
// its event naming no actor because no person removed it.
func TestAPushNoticeReachesTheCarrierAndAGoneDeviceIsRemoved(t *testing.T) {
	var svc contracts.Service
	var manifest module.Module
	var adapters push.Adapters
	inputs := pkit.NewModule("gone_inputs", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[contracts.RecipientLookup](w, directory{})
		pkit.Put[contracts.HostLookup](w, hosts{})
		adapters = push.Adapters{push.PlatformExpo: goneService{}}
		pkit.Put[push.Adapters](w, adapters)
		// The same answer the delivery case gives: notification needs the secret
		// port whether or not the channel under test ever reads a secret.
		pkit.Put[secrets.Secrets](w, secrets.Map(nil, nil))
		pkit.Put[jobs.TenantLister](w, acmeOnly)
		return module.Module{Name: "gone_inputs"}, nil
	}, pkit.Provides[contracts.RecipientLookup](), pkit.Provides[contracts.HostLookup](),
		pkit.Provides[push.Adapters](), pkit.Provides[secrets.Secrets](), pkit.Provides[jobs.TenantLister]())
	reader := pkit.NewModule("gone_reader", func(w *pkit.Wiring) (module.Module, error) {
		svc = pkit.Get[contracts.Service](w)
		for _, m := range pkit.Composition(w) {
			if m.Name == "notification" {
				manifest = m
			}
		}
		return module.Module{Name: "gone_reader"}, nil
	}, pkit.After(pkit.Everything), pkit.Needs[contracts.Service]())
	app := pkit.NewApp("gone-device").Use(inputs, notification.Module, reader)
	if err := app.Validate(pkit.Deployment{Environment: pkit.Development}); err != nil {
		t.Fatal(err)
	}
	_, conn := dbtest.Schema(t, notification.Migrations)
	if err := db.Run(tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), notificationtest.Ada),
		tenancy.Principal{UserID: notificationtest.Ada}), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := notification.Devices(adapters).RegisterDevice(ctx, tx, notificationtest.Ada,
			contracts.DeviceInput{Platform: push.PlatformExpo, Token: "ExponentPushToken[gone-device]"})
		if err != nil {
			return err
		}
		notice, err := svc.Notify(ctx, tx, contracts.Notice{
			Recipient: notificationtest.Ada, Title: "A notice for a device that is gone", Wants: contracts.WantsPush,
		})
		if err != nil {
			return err
		}
		payload, err := json.Marshal(contracts.DeliveryRequested{NotificationID: notice.ID, Recipient: notice.RecipientID})
		if err != nil {
			return err
		}
		sub := subscriptionNamed(t, contracts.EventPushRequested, manifest.Subscriptions)
		if err := sub.Handler(ctx, tx, events.Event{ID: uuid.New(), Name: contracts.EventPushRequested, Payload: payload}); err != nil {
			return err
		}
		if got := lastLedger(t, tx, notice.ID, contracts.ChannelPush); got.Outcome != "failed" {
			t.Errorf("a gone token left the push channel %q (%s), want failed", got.Outcome, got.Reason)
		}
		if _, err := crud.Get[*contracts.Device](tx, row.ID); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("the token the provider refused is still a device row: %v", err)
		}
		devices, total, err := notification.Devices(adapters).ListDevices(ctx, tx, notificationtest.Ada, crud.Query{})
		if err != nil {
			return err
		}
		if total != 0 || len(devices) != 0 {
			t.Errorf("the person's own list still shows the removed device: %d of %d", len(devices), total)
		}
		if ev := removedDevice(t, tx); ev.Actor != uuid.Nil || ev.DeviceID != row.ID || ev.Recipient != notificationtest.Ada {
			t.Errorf("the removal event names actor %s, device %s, recipient %s; want nobody, %s and Ada",
				ev.Actor, ev.DeviceID, ev.Recipient, row.ID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestAnotherPersonsDeviceIsNotTheirsToRemove walks the three refusals the
// device commands answer about ownership: a command that names no person owns
// nothing, another member of the tenant cannot remove the holder's device, and a
// person of another tenant cannot reach the row at all — the last one answered by
// row-level security rather than by a check, which is why it is asked here and
// not only in a fake. None of the three writes, publishes or keeps a row.
func TestAnotherPersonsDeviceIsNotTheirsToRemove(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	devices := notification.Devices(push.Adapters{push.PlatformExpo: goneService{}})
	owner := tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), notificationtest.Ada),
		tenancy.Principal{UserID: notificationtest.Ada})
	var id uuid.UUID
	if err := db.Run(owner, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := devices.RegisterDevice(ctx, tx, notificationtest.Ada,
			contracts.DeviceInput{Platform: push.PlatformExpo, Token: "ExponentPushToken[owned-device]"})
		if err != nil {
			return err
		}
		id = row.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	intruder := uuid.New()
	cases := []struct {
		name string
		ctx  context.Context
		call func(context.Context, db.Tx[db.Tenant]) error
	}{
		{"another member of the tenant", tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), intruder),
			tenancy.Principal{UserID: intruder}), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return devices.RemoveDevice(ctx, tx, id, notificationtest.Ada)
		}},
		{"a command that names nobody", tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), intruder), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := devices.RegisterDevice(ctx, tx, uuid.Nil, contracts.DeviceInput{
				Platform: push.PlatformExpo, Token: "ExponentPushToken[addressed-to-nobody]",
			})
			return err
		}},
		{"another tenant", tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), globex), notificationtest.Ada),
			tenancy.Principal{UserID: notificationtest.Ada}), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return devices.RemoveDevice(ctx, tx, id, notificationtest.Ada)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := db.Run(c.ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				before := outbox(t, tx)
				err := c.call(ctx, tx)
				if !errors.Is(err, crud.ErrNotFound) && !errors.Is(err, crud.ErrInvalid) && !errors.Is(err, tenancy.ErrPolicyDenied) {
					t.Errorf("refused %v", err)
				}
				if len(outbox(t, tx)) != len(before) {
					return errors.New("a refused device command published an event")
				}
				if db.TenantOf(tx).ID != acme.ID {
					return nil
				}
				if _, err := crud.Get[*contracts.Device](tx, id); err != nil {
					return err
				}
				return nil
			}); err != nil && !errors.Is(err, crud.ErrNotFound) && !errors.Is(err, tenancy.ErrPolicyDenied) {
				t.Fatal(err)
			}
		})
	}
}

// lastLedger is the newest row the delivery ledger holds for one channel.
func lastLedger(t *testing.T, tx db.Tx[db.Tenant], notice uuid.UUID, channel contracts.Channel) internal.Delivery {
	t.Helper()
	rows, err := internal.Deliveries(tx, notice)
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	var last internal.Delivery
	for _, d := range rows {
		if d.Channel == string(channel) {
			last = d
		}
	}
	return last
}

// removedDevice is the notification.device_removed this transaction published.
// The payload's own actor is the fact under test: the outbox's actor column
// carries whoever the transaction named, which for a provider's report is still
// the person whose delivery was being attempted, and the payload is where the
// module says that no person removed this device.
func removedDevice(t *testing.T, tx db.Tx[db.Tenant]) contracts.DeviceRemoved {
	t.Helper()
	var payload string
	if err := tx.DB().Table("platformkit_outbox").Select("payload").
		Where("name = ?", contracts.EventDeviceRemoved).Take(&payload).Error; err != nil {
		t.Fatalf("the removal published no %s event: %v", contracts.EventDeviceRemoved, err)
	}
	var ev contracts.DeviceRemoved
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		t.Fatalf("read the removal event: %v", err)
	}
	return ev
}

// TestATokenReregisteredByAnotherPersonBecomesTheirs is the other half of the
// same identity rule: the live token is the device, so a phone that changes
// hands stops carrying its previous owner's notices with the registration that
// moves it, and a person who registers the token that is already theirs gets
// their row back with nothing written and nothing published.
func TestATokenReregisteredByAnotherPersonBecomesTheirs(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	devices := notification.Devices(push.Adapters{push.PlatformExpo: goneService{}})
	in := contracts.DeviceInput{Platform: push.PlatformExpo, Token: "ExponentPushToken[sold-phone]"}
	ada := tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), notificationtest.Ada),
		tenancy.Principal{UserID: notificationtest.Ada})
	bob := tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), notificationtest.Bob),
		tenancy.Principal{UserID: notificationtest.Bob})
	var first uuid.UUID
	if err := db.Run(ada, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := devices.RegisterDevice(ctx, tx, notificationtest.Ada, in)
		if err == nil {
			first = row.ID
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(bob, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := devices.RegisterDevice(ctx, tx, notificationtest.Bob, in)
		if err != nil {
			return err
		}
		if row.ID != first || row.RecipientID != notificationtest.Bob {
			t.Errorf("the moved token is row %s of %s, want %s of Bob", row.ID, row.RecipientID, first)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(bob, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		before := outbox(t, tx)
		row, err := devices.RegisterDevice(ctx, tx, notificationtest.Bob, in)
		if err != nil {
			return err
		}
		if row.ID != first {
			t.Errorf("a person's own re-registration wrote a second row: %s, want %s", row.ID, first)
		}
		if len(outbox(t, tx)) != len(before) {
			return errors.New("a re-registration that changed nothing published an event")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
