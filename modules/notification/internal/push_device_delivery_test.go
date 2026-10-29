package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/push"
	pushlog "github.com/septagon-oss/platformkit/kit/push/providers/log"
	"github.com/septagon-oss/platformkit/kit/secrets"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestARegisteredDeviceReceivesThePersonsPushNotice(t *testing.T) {
	hub := pushlog.NewHub()
	providers := pkit.NewModule("delivery_inputs", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[contracts.RecipientLookup](w, directory{})
		pkit.Put[contracts.HostLookup](w, hosts{})
		pkit.Put[push.Adapters](w, hub.Adapters())
		// notification names secrets.Secrets as its need, so a composition of it
		// answers that need even to test a channel that never reads one: these
		// cases run the mailbox sender, which dials nothing.
		pkit.Put[secrets.Secrets](w, secrets.Map(nil, nil))
		pkit.Put[jobs.TenantLister](w, acmeOnly)
		return module.Module{Name: "delivery_inputs"}, nil
	}, pkit.Provides[contracts.RecipientLookup](), pkit.Provides[contracts.HostLookup](),
		pkit.Provides[push.Adapters](), pkit.Provides[secrets.Secrets](), pkit.Provides[jobs.TenantLister]())
	var svc contracts.Service
	var manifest module.Module
	reader := pkit.NewModule("delivery_reader", func(w *pkit.Wiring) (module.Module, error) {
		svc = pkit.Get[contracts.Service](w)
		for _, m := range pkit.Composition(w) {
			if m.Name == "notification" {
				manifest = m
			}
		}
		return module.Module{Name: "delivery_reader"}, nil
	}, pkit.After(pkit.Everything), pkit.Needs[contracts.Service]())
	app := pkit.NewApp("push-delivery").Use(providers, notification.Module, reader)
	if err := app.Validate(pkit.Deployment{Environment: pkit.Development}); err != nil {
		t.Fatal(err)
	}
	_, conn := dbtest.Schema(t, notification.Migrations)
	store := cache.Memory("push-delivery")
	t.Cleanup(func() { _ = store.Close() })
	api, router := httpx.New(httpx.Options{
		Conn: conn, Cache: store, Tenants: deviceRecipient{}, Authorize: deviceRecipient{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: notificationtest.Ada}, true, nil
		},
	})
	manifest.Routes(api.Surfaces(manifest.Name))
	list := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "http://acme.test/api/v1/notification/notifications", nil)
	listRequest.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "registered-person"})
	router.ServeHTTP(list, listRequest)
	if list.Code != http.StatusOK {
		t.Fatalf("signed-in notification list is unreachable: status=%d body=%s", list.Code, list.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "http://acme.test/api/v1/notification/notifications/devices",
		strings.NewReader(`{"platform":"expo","token":"ExponentPushToken[registered-device]"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://acme.test")
	request.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "registered-person"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code < 200 || response.Code >= 300 {
		t.Fatalf("signed-in device registration: status=%d body=%s; want a successful registration", response.Code, response.Body.String())
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Notify(ctx, tx, contracts.Notice{
			Recipient: notificationtest.Ada, Title: "A notice for this device", Wants: contracts.WantsPush,
		})
		if err != nil {
			return err
		}
		payload, err := json.Marshal(contracts.DeliveryRequested{NotificationID: row.ID, Recipient: row.RecipientID})
		if err != nil {
			return err
		}
		sub := subscriptionNamed(t, contracts.EventPushRequested, manifest.Subscriptions)
		return sub.Handler(ctx, tx, events.Event{ID: uuid.New(), Name: contracts.EventPushRequested, Payload: payload})
	}); err != nil {
		t.Fatal(err)
	}
	sent := hub.Sent()
	if len(sent) != 1 {
		t.Fatalf("notification reached the composed push adapter %d times, want one", len(sent))
	}
	if sent[0].Device.UserID != notificationtest.Ada || sent[0].Device.TenantID != acme.ID || sent[0].Message.Title != "A notice for this device" {
		t.Errorf("push delivery lost the recipient, tenant or message: %+v", sent[0])
	}
}

type deviceRecipient struct{}

func (deviceRecipient) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return acme, nil
}

func (deviceRecipient) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return false, nil
}
