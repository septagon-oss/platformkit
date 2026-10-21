package internal

import (
	"context"
	"errors"
	"strings"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// pushCarrier is one of the two channels that reach a device rather than an
// inbox: it resolves the addresses this module owns and hands each one to the
// port, which is the only thing that knows the protocol.
//
// The split is the reason the channel could not be carried before the device
// table existed and is not carried without one. contracts.Provider is handed an
// empty Delivery.Target — a token, a browser subscription and a tenant's endpoint
// URL are three different things to store — so this half reads the recipient's
// own live rows in the delivery's transaction, under that transaction's tenant,
// and the port never learns that a notification, a recipient or a tenant exists:
// it is given one resolved device and one rendered message.
//
// What a person holds several devices means for the ledger: the channel is sent
// when any of them took it, and every device that refused is accounted for in the
// reason. It answers contracts.Permanent when none of them could be carried — a
// terminal failed row and an acknowledged event, which is what a fact no retry
// changes owes the queue — and returns a bare error when one of them failed for a
// reason that is about the provider's afternoon rather than the device, so the
// transaction rolls back, the gone tokens stay, and the outbox tries again.
type pushCarrier struct {
	channel  contracts.Channel
	adapters push.Adapters
}

// PushCarrier is the contracts.Provider for one channel, or nil when this
// installation carries no adapter that reaches it — which is the same answer a
// missing key in contracts.Providers gives, so a deployment with no push has one
// reading of that fact rather than two.
func PushCarrier(channel contracts.Channel, adapters push.Adapters) contracts.Provider {
	for _, platform := range push.Platforms {
		if carries(channel, platform) && adapters.Get(platform) != nil {
			return &pushCarrier{channel: channel, adapters: adapters}
		}
	}
	return nil
}

// carries is where a channel and a platform meet, and it is the module's table
// rather than the port's: kit/push knows carrier families and no channel, and
// this module knows channels and no protocol. A browser subscription is the web
// channel; every phone family — Expo today, FCM and APNs the day an adapter is
// composed for them — is the push one.
func carries(channel contracts.Channel, platform push.Platform) bool {
	web := platform == push.PlatformWeb
	return web == (channel == contracts.ChannelWebPush)
}

func (c *pushCarrier) Channel() contracts.Channel { return c.channel }

func (c *pushCarrier) Deliver(ctx context.Context, tx db.Tx[db.Tenant], d contracts.Delivery) error {
	notice, err := crud.Get[*contracts.Notification](tx, d.NotificationID)
	if err != nil {
		return err
	}
	devices, err := devicesOf(tx, notice.RecipientID, c.channel)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return contracts.Permanent("the recipient has no " + string(c.channel) + " device registered")
	}
	// The notification id is the collapse key, which is what makes an outbox
	// redelivery one visible notification rather than two: the platform coalesces
	// an undelivered message carrying the same tag.
	message := push.Message{Title: d.Subject, Body: d.Text, URL: d.Link, Tag: d.NotificationID.String()}
	tenant := db.TenantOf(tx)
	var refusals []string
	for _, device := range devices {
		_, err := c.adapters.Send(ctx, push.Device{
			ID: device.ID, TenantID: tenant.ID, UserID: notice.RecipientID,
			Platform: device.Platform, Token: device.Token,
		}, message)
		switch {
		case err == nil:
		case errors.Is(err, push.ErrGone):
			// The provider no longer knows this token, so the row is deleted in
			// this same transaction: a gone token that is kept is a device that is
			// never re-registered and a channel that fails forever.
			reason := push.Redact(err, device.Token).Error()
			if err := forgetGone(ctx, tx, device, reason); err != nil {
				return err
			}
			refusals = append(refusals, reason)
		case errors.Is(err, push.ErrMalformed), errors.Is(err, push.ErrRejected), errors.Is(err, push.ErrInvalid):
			// Something about this device or this copy, and no retry will change
			// either: the row stays, because the person's own client can
			// re-register and the next notice will then work.
			refusals = append(refusals, push.Redact(err, device.Token).Error())
		default:
			return err
		}
	}
	if len(refusals) == len(devices) {
		return contracts.Permanent(strings.Join(refusals, "; "))
	}
	return nil
}
