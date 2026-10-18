package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/push/providers/expo"
	"github.com/septagon-oss/platformkit/kit/push/providers/webpush"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
)

func TestDeviceRegistrationRefusesTokensItsAdapterCannotCarry(t *testing.T) {
	for _, c := range []struct {
		platform push.Platform
		token    string
		adapters push.Adapters
	}{
		{push.PlatformExpo, "not an Expo token", expo.New(expo.Config{})},
		{push.PlatformWeb, "not a subscription", webpush.New(webpush.Config{})},
	} {
		t.Run(string(c.platform), func(t *testing.T) {
			if err := c.adapters.Get(c.platform).Validate(c.token); !errors.Is(err, push.ErrMalformed) {
				t.Fatalf("adapter control: want malformed token, got %v", err)
			}
			_, conn := dbtest.Schema(t, notification.Migrations)
			ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), acme), tenancy.Principal{UserID: notificationtest.Ada})
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				row, err := notification.Devices(c.adapters).RegisterDevice(ctx, tx, notificationtest.Ada,
					contracts.DeviceInput{Platform: c.platform, Token: c.token})
				if (!errors.Is(err, crud.ErrInvalid) && !errors.Is(err, push.ErrMalformed)) || row != nil {
					t.Errorf("uncarryable token: error=%v returned row=%v; want invalid/malformed and no row", err, row != nil)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				count, err := crud.Count[*contracts.Device](tx)
				if err != nil {
					return err
				}
				if count != 0 || len(outbox(t, tx)) != 0 {
					t.Errorf("refused registration committed %d devices and %d events; want neither", count, len(outbox(t, tx)))
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
