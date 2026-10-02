package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// Carrier is the subscription that asks one wired provider to carry one channel.
//
// It exists because "this deployment has a push adapter" is a fact about the
// composition, and a fact about the composition that opens a `requested` row and
// publishes an event nobody subscribed to is the hole the ledger was built to
// close: the row would stand open forever, coverage would stop counting it, and
// the only record of the delivery would be a log line. So the manifest composes
// one of these per provider in Deps.Providers, named by the same table
// (contracts.RequestedEvent) Notify asks a channel by, and the module writes the
// ledger row around the answer rather than trusting the carrier to write it —
// which is what makes a stub that only records an honest carrier: a stub that
// forgets cannot make a notice unaccounted for.
//
// The bookkeeping is SendMail's, and it is the same for a reason: the payload is
// three identifiers, so the notice is read back inside this event's own tenant
// transaction, a notice that is gone is a suppression rather than a failure, a
// channel the ledger already closed is acknowledged without a second send, and
// the provider's answer decides the terminal row — nil is `sent`,
// contracts.ErrPermanent is `failed` with its reason and acknowledges the event,
// anything else returns the error so the transaction rolls back and the outbox
// retries on the kernel's ladder.
//
// What this subscription does *not* do is resolve the handle the channel
// actually reaches. A device token, a browser subscription and a tenant's
// endpoint URL all live outside this module today (contracts.ChannelPush,
// ChannelWebPush, ChannelWebhook), so Delivery.Target is empty here and the
// carrier resolves its own in the transaction it is handed — and a carrier with
// nothing to address this person returns contracts.Permanent with that sentence,
// which is a terminal `failed` row and an acknowledged event, not a retry ladder
// for a fact no retry will change.
func Carrier(p contracts.Provider, hosts contracts.HostLookup, secure bool) events.Subscription {
	ch := p.Channel()
	name, ok := contracts.RequestedEvent[ch]
	if !ok {
		// The composition wired a carrier for a channel no worker is ever asked
		// for — in-app, which is the row itself, or mail, which this module's own
		// subscription carries. It fails here, naming the channel, rather than as
		// two subscriptions on one event name.
		panic("notification: Carrier: no worker is asked for the channel " + string(ch))
	}
	return events.Subscription{
		Module: "notification", Name: name,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			var req contracts.DeliveryRequested
			if err := json.Unmarshal(ev.Payload, &req); err != nil {
				return fmt.Errorf("notification: read the %s request: %w", ch, err)
			}
			row, err := crud.Get[*contracts.Notification](tx, req.NotificationID)
			if errors.Is(err, crud.ErrNotFound) {
				slog.InfoContext(ctx, "notification: the notice was gone before its delivery was sent",
					"notification", req.NotificationID, "channel", string(ch), "recipient", req.Recipient)
				return record(tx, req.NotificationID, string(ch), OutcomeSuppressed,
					"the notice was deleted before its "+string(ch)+" was sent")
			}
			if err != nil {
				return err
			}
			done, err := closed(tx, row.ID, string(ch))
			if err != nil {
				return err
			}
			if done {
				slog.InfoContext(ctx, "notification: the delivery was already carried; the redelivery was acknowledged",
					"notification", row.ID, "channel", string(ch))
				return nil
			}
			base, err := baseURL(ctx, tx, hosts, secure)
			if err != nil {
				return err
			}
			text, err := render(row, base)
			if err != nil {
				return err
			}
			err = p.Deliver(ctx, tx, contracts.Delivery{
				NotificationID: row.ID, Channel: ch, Subject: row.Title,
				Text: text, Link: absolute(base, row.Link), Lang: "en",
			})
			if errors.Is(err, contracts.ErrPermanent) {
				return record(tx, row.ID, string(ch), OutcomeFailed, reason(err))
			}
			if err != nil {
				return err
			}
			return record(tx, row.ID, string(ch), OutcomeSent, "")
		},
	}
}

// absolute is the notice's link as a URL a client outside the application can
// follow, built on the recipient's own tenant host. A notice with no link, or a
// deployment with no host to build on, is the row's own path or nothing.
func absolute(base, path string) string {
	if path == "" || base == "" {
		return path
	}
	return strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(path, "/")
}
