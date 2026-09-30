package contracts

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// Channel is one way a person can be told something. The values are the CHECK
// constraint of notification_deliveries (migrations/000027, widened by this
// module's 000028 to take web_push) spelled once in Go, so a provider written
// outside this module can name a channel and cannot invent one the ledger would
// refuse.
//
// They are also the whole of what the ledger can say a notice was asked for:
// a channel with no row is a channel nobody requested.
type Channel string

const (
	// ChannelInApp is the notification row itself. It is a channel so that the
	// ledger answers "was she told at all" with one query, and it is the one
	// channel no preference and no tenant switch can refuse — see Decide.
	ChannelInApp Channel = "in_app"
	// ChannelEmail is go-mail over the composed SMTP relay, signed as the
	// tenant's own sender.
	ChannelEmail Channel = "email"
	// ChannelPush is a mobile device token (the token table is this module's,
	// the FCM/APNs adapter is T-0120's).
	ChannelPush Channel = "push"
	// ChannelWebPush is a browser subscription, sent over VAPID.
	ChannelWebPush Channel = "web_push"
	// ChannelWebhook is the tenant's own endpoint, HMAC-signed.
	ChannelWebhook Channel = "webhook"
)

// Channels lists every channel, in the order the ledger writes them and the
// order Decide answers in. Anything that iterates channels iterates this, so a
// new channel is one constant and one element here.
var Channels = []Channel{ChannelInApp, ChannelEmail, ChannelPush, ChannelWebPush, ChannelWebhook}

// Validate reports whether this is one of the channels the ledger knows.
func (c Channel) Validate() error {
	if _, ok := channelBits[c]; !ok {
		return fmt.Errorf("notification: %q is not a channel", c)
	}
	return nil
}

// Wants is what the module raising a notice asks for, as a ceiling and never a
// floor beyond in-app: the person's preferences, the tenant's switches and the
// tenant's sender then decide which of the asked-for channels actually happen
// (Decide). Its zero value is in-app alone, which is what a caller that says
// nothing means: the person is told in the application, and nothing leaves it.
type Wants uint8

// The bits, one per channel. WantsAll is every channel; a caller that wants
// mail and nothing else writes WantsInApp | WantsEmail.
const (
	WantsInApp Wants = 1 << iota
	WantsEmail
	WantsPush
	WantsWebPush
	WantsWebhook

	WantsAll = WantsInApp | WantsEmail | WantsPush | WantsWebPush | WantsWebhook
)

// channelBits is the channel-to-bit map, in Channels' order. It is the only
// place a channel name and a bit meet.
var channelBits = map[Channel]Wants{
	ChannelInApp:   WantsInApp,
	ChannelEmail:   WantsEmail,
	ChannelPush:    WantsPush,
	ChannelWebPush: WantsWebPush,
	ChannelWebhook: WantsWebhook,
}

// Has reports whether this bit is set.
func (w Wants) Has(c Wants) bool { return w&c == c && c != 0 }

// HasChannel reports whether this channel was asked for. An unknown channel is
// never asked for, which is what makes Wants closed over Channels.
func (w Wants) HasChannel(c Channel) bool { return w.Has(channelBits[c]) }

// AsChannels is the asked-for set as a list, in Channels' order, and it always
// starts with in-app: the row is the notice, so no ceiling a caller raises can
// fall below telling the person in the application. A caller that asks for
// mail alone gets in-app and mail, which is what the ledger records and what
// delivery_ledger_coverage then counts.
func (w Wants) AsChannels() []Channel {
	out := make([]Channel, 0, len(Channels))
	for _, c := range Channels {
		if c == ChannelInApp || w.HasChannel(c) {
			out = append(out, c)
		}
	}
	return out
}

// The outcomes of the delivery ledger, spelled from the same CHECK constraint.
// OutcomeRequested opens a channel and the other three close it: Coverage counts
// a requested channel covered when one of the terminal rows exists, so a channel
// that ends must end with one of these and nothing else.
const (
	OutcomeRequested  = "requested"
	OutcomeSent       = "sent"
	OutcomeSuppressed = "suppressed"
	// OutcomeFailed is a channel that ended because a provider could not and
	// never could carry it — the relay refused the address, the push token is
	// gone, the tenant's sender is unverified. It existed in the table's CHECK
	// before anything in Go could write it, which is how a numerator drifts.
	OutcomeFailed = "failed"
)

// ErrPermanent distinguishes the two kinds of thing a provider is told by
// somebody else's machine, and the ledger's shape depends on which it is.
//
// A transient error — a connection dropped, a 4xx from a relay that will
// answer again — is returned as-is: the transaction rolls back, no terminal row
// is written, and the outbox retries on the kernel's ladder. A permanent one —
// 550 no such mailbox, a push token that no longer exists, an endpoint that
// answers 410 — is wrapped in ErrPermanent and handled the other way round: the
// worker records a terminal failed row with the reason and returns nil, so the
// message is acknowledged and never retried. A permanent failure that is
// retried is a channel with no terminal row until the dead letter, which is the
// hole this closes; a transient one that is recorded as failed is a lie about a
// delivery that happened ten minutes later.
var ErrPermanent = errors.New("permanent")

// Permanent wraps a provider's reason as a refusal no further attempt could
// change. The reason is what lands in notification_deliveries.reason, so it is
// the sentence an operator reads.
func Permanent(reason string) error { return fmt.Errorf("%w: %s", ErrPermanent, reason) }

// Delivery is one addressed message: this module has decided the channel, found
// the target and rendered the words, and a Provider's job is only to carry it.
//
// Everything a provider needs is here and nothing it does not. It carries no
// tenant id and no recipient beyond Target: the transaction it is handed is the
// recipient's tenant's, and a provider that could address another tenant would
// be a provider that reads outside its own row-level security policy.
type Delivery struct {
	// NotificationID is the row this delivery describes, which is what the
	// worker writes into the ledger when the provider answers.
	NotificationID uuid.UUID
	// Channel is which of the requested channels this is, and it is the one
	// Deliver answers for: one Delivery is one channel, never a set.
	Channel Channel
	// Target is the address, device token or endpoint URL this module resolved.
	Target string
	// Subject is the one line — the mail subject, the push title, the webhook
	// "what happened".
	Subject string
	// Text is the plain-text body every channel carries; HTML is its
	// alternative when the channel can show one and "" when it cannot.
	Text, HTML string
	// Link is the absolute URL of Notice.Link, built on the recipient's own
	// tenant host (see HostLookup).
	Link string
	// Lang is the BCP-47 language Text and HTML were rendered in.
	Lang string
	// Sender is the tenant's own sender, for the channels that present
	// themselves as a person rather than as the platform: email today. It is
	// nil for a channel that has no sender concept.
	Sender *Sender
	// Secret is the webhook's HMAC key, nil for every other channel.
	Secret []byte
}

// Provider carries one channel to its target.
//
// It is the port the brief asks for, and the boundary is the whole of its
// design: this module owns who gets told (preferences, devices, senders, the
// ledger) and a provider owns only how, so no provider reads a preference,
// writes a ledger row, or learns that another channel exists. The module writes
// every ledger row around Deliver — before it where a channel cannot even be
// addressed, after it where the provider answered.
//
// The implementations are composed in apps/platformkit: go-mail for
// ChannelEmail, a VAPID sender for ChannelWebPush, T-0120's adapter for
// ChannelPush, an HTTP client for ChannelWebhook. A stub still owes the ledger
// an answer, which is why the ledger is written by the module and not by the
// stub: a provider that forgets cannot make a notice unaccounted for.
//
// The error contract is ErrPermanent above. Deliver must not block past the
// context's deadline — the worker's whole transaction is inside it.
type Provider interface {
	Channel() Channel
	Deliver(ctx context.Context, tx db.Tx[db.Tenant], d Delivery) error
}

// Providers is the composition's answer to "what does this deployment send", one
// per channel. A channel missing from it is not broken: Decide suppresses it with
// a reason the ledger keeps, so a deployment that sends mail and nothing else
// accounts for every push it was asked for.
type Providers map[Channel]Provider

// Get is the provider for a channel, or nil.
func (p Providers) Get(c Channel) Provider { return p[c] }
