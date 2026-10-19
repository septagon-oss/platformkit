package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

func TestPreferenceCommandsRefuseAnotherRecipientsChoices(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	prefs := notification.Settings()
	owner := notificationtest.Ada
	ownCtx := tenancy.WithPrincipal(asAdmin(tenancy.WithTenant(t.Context(), acme)), tenancy.Principal{UserID: owner})
	if err := db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := prefs.SetChannel(ctx, tx, owner, "", contracts.ChannelEmail, false); err != nil {
			return err
		}
		_, err := prefs.SetQuietHours(ctx, tx, contracts.QuietHours{RecipientID: owner, StartMinute: 1320, EndMinute: 420, TimeZone: "UTC"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	intruder := uuid.New()
	ctx := tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), intruder), tenancy.Principal{UserID: intruder})
	attempts := []struct {
		name string
		call func(*testing.T, context.Context, db.Tx[db.Tenant]) error
	}{
		{"channel", func(t *testing.T, ctx context.Context, tx db.Tx[db.Tenant]) error {
			row, err := prefs.SetChannel(ctx, tx, owner, "", contracts.ChannelEmail, true)
			if row != nil {
				t.Error("refused preference command returned another recipient's row")
			}
			return err
		}},
		{"quiet hours", func(t *testing.T, ctx context.Context, tx db.Tx[db.Tenant]) error {
			row, err := prefs.SetQuietHours(ctx, tx, contracts.QuietHours{RecipientID: owner, StartMinute: 1, EndMinute: 2, TimeZone: "UTC"})
			if row != nil {
				t.Error("refused quiet-hours command returned another recipient's row")
			}
			return err
		}},
		{"clear quiet hours", func(t *testing.T, ctx context.Context, tx db.Tx[db.Tenant]) error {
			return prefs.ClearQuietHours(ctx, tx, owner)
		}},
	}
	for _, attempt := range attempts {
		t.Run(attempt.name, func(t *testing.T) {
			err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				before := len(outbox(t, tx))
				if err := attempt.call(t, ctx, tx); !errors.Is(err, tenancy.ErrPolicyDenied) {
					t.Errorf("another recipient's %s: got %v, want policy denial", attempt.name, err)
				}
				choices, err := (internal.Prefs{}).Settings(ctx, tx, owner, "")
				if err != nil {
					return err
				}
				if len(choices) != 1 || choices[0].Enabled {
					t.Error("refused command changed the recipient's opt-out")
				}
				quiet, err := (internal.Prefs{}).Quiet(ctx, tx, owner)
				if err != nil {
					return err
				}
				if quiet == nil || quiet.StartMinute != 1320 || quiet.EndMinute != 420 {
					t.Error("refused command changed the recipient's quiet window")
				}
				if after := len(outbox(t, tx)); after != before {
					t.Errorf("refused command published %d events", after-before)
				}
				return errRollback
			})
			if !errors.Is(err, errRollback) {
				t.Fatal(err)
			}
		})
	}
}
