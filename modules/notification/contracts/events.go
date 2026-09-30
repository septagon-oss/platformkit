package contracts

import (
	"time"

	"github.com/google/uuid"
)

// The events this module emits. A subscriber names one of these constants rather
// than a string, so renaming one is a compile error where it is listened for.
//
// EventEmailRequested is the odd one, and it is the point of this module's
// shape: it is how "and send it by mail" leaves the request. The row and the
// event commit together in the caller's transaction and the worker renders and
// sends, so a request never waits on somebody else's machine, a mail server
// that is down is retried by the outbox, and a message that can never be sent
// ends in the kernel's dead letters where somebody can read it.
const (
	EventCreated        = "notification.created"
	EventEmailRequested = "notification.email_requested"
	EventRead           = "notification.read"

	// The other channels, named one per event on purpose. One event carrying a
	// channel field would be one retry ladder for four transports: a tenant's
	// endpoint that answers 500 all afternoon would hold its own notices'
	// mail back with it, and the mail was fine. One name per channel is one
	// outbox row per channel, so a webhook that is down delays nothing but
	// webhooks.
	//
	// EventEmailRequested keeps the name it shipped with: an installation's
	// outbox has rows with it, subscriptions are named by it, and renaming a
	// live event is a migration of somebody's queue rather than of this file.
	EventPushRequested    = "notification.push_requested"
	EventWebPushRequested = "notification.web_push_requested"
	EventWebhookRequested = "notification.webhook_requested"

	// The settings events. A preference change, a sender change and a device
	// registration are all state changes this module introduces, and modules/
	// audit records published events rather than rows, so publishing is how
	// they are audited (actor, tenant, moment, cause — the audit module's own
	// shape) and how they are not.
	EventPreferenceSet  = "notification.preference_set"
	EventSenderSet      = "notification.sender_set"
	EventSenderVerified = "notification.sender_verified"
	EventDeviceAdded    = "notification.device_added"
	EventDeviceRevoked  = "notification.device_revoked"
)

// Events is every event this module emits, for the manifest.
var Events = []string{
	EventCreated, EventEmailRequested, EventRead,
	EventPushRequested, EventWebPushRequested, EventWebhookRequested,
	EventPreferenceSet, EventSenderSet, EventSenderVerified,
	EventDeviceAdded, EventDeviceRevoked,
}

// RequestedEvent is the event a chosen channel is asked for by — the one table
// a channel and an event name meet, so Notify and the manifest never disagree.
// in-app has no entry's twin: the row is the delivery, so nothing is asked for.
var RequestedEvent = map[Channel]string{
	ChannelEmail:   EventEmailRequested,
	ChannelPush:    EventPushRequested,
	ChannelWebPush: EventWebPushRequested,
	ChannelWebhook: EventWebhookRequested,
}

// Created is the payload of EventCreated: somebody was told something.
type Created struct {
	NotificationID uuid.UUID `json:"notificationId"`
	Recipient      uuid.UUID `json:"recipientId"`
	Title          string    `json:"title"`
	At             time.Time `json:"at"`
}

// EmailRequested is the payload of EventEmailRequested, and it carries two
// identifiers and nothing else.
//
// It used to carry the whole message — the address, the title, the body, the
// link — which saved the worker a query and cost something worth more than the
// query. An outbox row is kept for a week and copied into every subscriber's
// trail: modules/audit records every event this application publishes, so a
// payload with a body in it is the body of every notice in a table nobody
// treats as a mailbox, and a payload with an address in it is a mailing list.
// A reset link in one would be a live credential in the audit trail.
//
// So the worker reads the row back inside the event's own tenant transaction
// and resolves the address there. The consequences are named rather than
// hidden: a notice deleted before the worker reaches it is a skip and a log
// line, and an address changed in between is the address the mail goes to,
// which is the newer of the two answers and the one a person expects.
//
// The convention this makes explicit for one event is the one modules/audit
// relies on for all of them: an event carries identifiers, not content.
type EmailRequested struct {
	NotificationID uuid.UUID `json:"notificationId"`
	Recipient      uuid.UUID `json:"recipientId"`
	At             time.Time `json:"at"`
}

// Read is the payload of EventRead: the recipient has seen it.
type Read struct {
	NotificationID uuid.UUID `json:"notificationId"`
	Recipient      uuid.UUID `json:"recipientId"`
	At             time.Time `json:"at"`
}

// DeliveryRequested is the payload of every channel request but mail's, and it
// is the same three identifiers for the same reason: an outbox row is kept for a
// week and copied into the audit trail, so it carries who and which, never what.
// The worker reads the row back in the event's own tenant transaction.
type DeliveryRequested struct {
	NotificationID uuid.UUID `json:"notificationId"`
	Recipient      uuid.UUID `json:"recipientId"`
	At             time.Time `json:"at"`
}

// PreferenceSet is the payload of EventPreferenceSet: one person changed one
// answer about one channel. It is audited because a person who says "I never
// turned mail off" is answered from the trail, and it carries no address, which
// is the convention every payload here follows.
type PreferenceSet struct {
	Recipient uuid.UUID `json:"recipientId"`
	Intent    string    `json:"intent,omitempty"`
	Channel   Channel   `json:"channel"`
	Enabled   bool      `json:"enabled"`
	At        time.Time `json:"at"`
}

// SenderSet is the payload of EventSenderSet, and it names the sender without
// the key, the token or the proof: the row it describes carries those, and an
// audit trail is not a keystore.
type SenderSet struct {
	SenderID uuid.UUID `json:"senderId"`
	Domain   string    `json:"domain"`
	Selector string    `json:"selector"`
	Status   string    `json:"status"`
	Actor    uuid.UUID `json:"actorId"`
	At       time.Time `json:"at"`
}

// Verified is the payload of EventSenderVerified: this tenant may send
// from this address as of this moment, which is the fact a delivery dispute
// turns on.
type Verified struct {
	SenderID uuid.UUID `json:"senderId"`
	Domain   string    `json:"domain"`
	Selector string    `json:"selector"`
	Actor    uuid.UUID `json:"actorId"`
	At       time.Time `json:"at"`
}

// DeviceChanged is the payload of EventDeviceAdded and EventDeviceRevoked. It
// names the device and its platform and never the token: a push token is what
// anybody needs to put a message on somebody's screen, which puts it with the
// credentials that may not be copied into a trail (see EmailRequested).
type DeviceChanged struct {
	DeviceID  uuid.UUID `json:"deviceId"`
	Recipient uuid.UUID `json:"recipientId"`
	Platform  string    `json:"platform"`
	At        time.Time `json:"at"`
}
