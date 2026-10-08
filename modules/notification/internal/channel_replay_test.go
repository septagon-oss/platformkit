package internal_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/notification/internal"
)

type replayCarrier struct {
	carries
	channel contracts.Channel
}

func (p *replayCarrier) Channel() contracts.Channel { return p.channel }

func TestEveryCarriedChannelIgnoresACommittedEventReplay(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	for _, channel := range []contracts.Channel{contracts.ChannelEmail, contracts.ChannelPush, contracts.ChannelWebPush, contracts.ChannelWebhook} {
		t.Run(string(channel), func(t *testing.T) {
			carrier := &replayCarrier{channel: channel}
			mailbox := notification.NewMailbox()
			deps := notification.Deps{Recipients: directory{}, Mailer: mailbox}
			if channel != contracts.ChannelEmail {
				deps.Providers = contracts.Providers{channel: carrier}
			}
			svc, mod := notification.Module(deps)
			sub := subscriptionNamed(t, contracts.RequestedEvent[channel], mod.Subscriptions)
			ctx := tenancy.WithTenant(t.Context(), acme)
			var noticeID uuid.UUID
			var event events.Event
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				row, err := svc.Notify(ctx, tx, contracts.Notice{Recipient: notificationtest.Ada, Title: "One delivery", Wants: contracts.WantsEmail | contracts.WantsPush | contracts.WantsWebPush | contracts.WantsWebhook})
				if err != nil {
					return err
				}
				noticeID = row.ID
				payload, err := json.Marshal(contracts.DeliveryRequested{NotificationID: row.ID, Recipient: row.RecipientID})
				event = events.Event{ID: uuid.New(), Name: sub.Name, Payload: payload}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			var first []internal.Delivery
			for attempt := range 2 {
				if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					if err := sub.Handler(ctx, tx, event); err != nil {
						return err
					}
					rows, err := internal.Deliveries(tx, noticeID)
					if err != nil {
						return err
					}
					if attempt == 0 {
						first = rows
					} else if !reflect.DeepEqual(first, rows) {
						t.Errorf("replayed event changed the ledger: before %v, after %v", first, rows)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			delivered := len(carrier.got)
			if channel == contracts.ChannelEmail {
				delivered = len(mailbox.Sent())
			}
			if delivered != 1 {
				t.Errorf("same event delivered %d times, want once", delivered)
			}
		})
	}
}
