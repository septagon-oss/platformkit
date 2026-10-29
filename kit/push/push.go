// Package push is the transport below a notification module's provider: it
// carries one rendered message to one device whose token somebody already
// resolved. It owns no device store, no preference, no ledger and no copy — who
// is told what, and whether they wanted it, are answered before a Message exists.
//
// It is a kernel kit rather than a method on a module's provider interface for
// the reason mail has its own provider package: an SDK stays out of a module's
// contracts/, and only an adapter's package imports a platform's protocol.
//
// The five sentinels split into three answers, and the split is the design: a token
// the provider no longer knows (ErrGone) means the caller's row is a lie and belongs
// deleted; a token this adapter cannot carry, or a refused message, means the row
// stays; anything outside the vocabulary says nothing about the device, so the
// caller retries. Mapping a transient failure to ErrGone deletes a whole bad hour.
package push

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"github.com/septagon-oss/platformkit/kit/telemetry"
)

// Platform is the carrier family a device speaks to. Its values are the CHECK
// constraint of the device table spelled in Go, so a router, a store and a
// migration cannot drift apart on which platforms exist.
type Platform string

const (
	// PlatformWeb is a browser subscription, carried over Web Push with VAPID
	// (RFC 8030, RFC 8291, RFC 8292).
	PlatformWeb Platform = "web"
	// PlatformExpo is an Expo push token, which reaches the FCM and APNs
	// services behind Expo's own endpoint.
	PlatformExpo Platform = "expo"
	// PlatformFCM is Google's Firebase Cloud Messaging endpoint directly. Named
	// so a device row can exist the day an adapter is composed for it; nothing
	// in this repository carries it today.
	PlatformFCM Platform = "fcm"
	// PlatformAPNS is Apple's Push Notification service directly, and named for
	// the same reason as PlatformFCM.
	PlatformAPNS Platform = "apns"
)

// Platforms is every platform the port knows, in the order a router lists them
// and a suite runs them in — the same duty notification/contracts.Channels does
// for channels.
var Platforms = []Platform{PlatformWeb, PlatformExpo, PlatformFCM, PlatformAPNS}

// Validate reports whether this is one of the platforms the port knows. An
// unknown spelling is refused before it reaches a store, so a device row is
// never written for a platform no adapter could ever carry.
func (p Platform) Validate() error {
	for _, known := range Platforms {
		if p == known {
			return nil
		}
	}
	return fmt.Errorf("%w: %q is not a platform this port speaks", ErrInvalid, string(p))
}

// Device is one addressable thing: an opaque token, what speaks it, and the two
// identities it belongs to.
//
// Token is opaque *here* on purpose. Its shape is the adapter's — a browser
// subscription is a JSON object and an Expo token is a bracketed string — which
// is why both share one string field and why no caller has to know which is
// which. The two ids are what a send's span and its log line carry: an operator
// reading a trace asks which tenant's person was not reached, and the only
// answer available to a transport that owns no store is the one the caller puts
// in the device.
type Device struct {
	// ID is the store's id for the row, or uuid.Nil for a device that came from
	// anywhere else.
	ID uuid.UUID
	// TenantID is whose tenant's person this device belongs to.
	TenantID uuid.UUID
	// UserID is the person it belongs to.
	UserID uuid.UUID
	// Platform is the routing key and the only fact about a device anything
	// branches on.
	Platform Platform
	// Token is the provider's token or serialised subscription.
	Token string
}

// Message is one rendered notification. The words arrive already rendered:
// this package carries no copy, no template and no language, because the
// notification module's templates rendered them and the translation module owns
// the words.
type Message struct {
	// Title is the one line.
	Title string
	// Body is the text under it, carried no longer than MaxBody bytes.
	Body string
	// URL is the deep link. Each adapter puts it where its platform reads one.
	URL string
	// Tag is the collapse key: a provider coalesces an undelivered message with
	// the same tag rather than adding a second one. The notification module sets
	// it to the notification id, which is what makes an outbox redelivery one
	// notification. No platform's collapsing reaches a notification already on
	// screen the way the client's own thread id does; that stays the client's.
	Tag string
	// TTL is how long the provider may keep trying. Zero means MaxTTL: the
	// default is chosen here rather than left to the platforms, whose own
	// defaults run to weeks, and a notice that arrives a month late is noise.
	TTL time.Duration
}

// MaxBody is the longest body any adapter carries, in bytes. Every push
// transport this port speaks has a limit near this size — Expo's payload
// reaches Apple and Google with a total of at most 4 KiB, and Web Push's
// content-coding record size is capped at 4096 — and a notice's body arrives
// unbounded, so the bound lives at the transport that has to honour it.
const MaxBody = 4096

// MaxTTL is the longest a provider is ever asked to keep trying: one day. Both
// transports have longer defaults of their own, and this constant exists to
// override rather than inherit them.
const MaxTTL = 24 * time.Hour

// MaxTag is the longest collapse key an adapter puts on the wire. It is a
// length, not a namespace: the port puts no scheme on what a tag means, and the
// notification module uses its own notification id.
const MaxTag = 256

// Clip is the message with its Body shortened to MaxBody bytes at a rune
// boundary. Every adapter calls it before it sends: a notice whose body runs
// over is told shortly rather than refused, because the alternative is that
// somebody is not told at all. A caller reads whether anything moved by
// comparing the body it passed with what the send records on the span
// (push.truncated), which is where a clipped notice stays visible.
func (m Message) Clip() Message {
	if len(m.Body) <= MaxBody {
		return m
	}
	return m.TrimTo(len(m.Body) - MaxBody)
}

// TrimTo is the message with its Body shortened by n bytes at a rune boundary,
// to nothing at worst. An adapter that has a shorter ceiling of its own — a
// platform that caps the whole notification, title and link included — reaches
// for this rather than MaxBody, so the copy that goes is the copy that fits.
func (m Message) TrimTo(n int) Message {
	cut := len(m.Body) - n
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !utf8.RuneStart(m.Body[cut]) {
		cut--
	}
	m.Body = m.Body[:cut]
	return m
}

// Validate is the port's own check, run by every adapter before it opens a
// socket: a title or a body (one of them, at least), a tag no longer than
// MaxTag bytes, a TTL that is not negative, a URL that parses or is empty.
//
// Nothing here is a size question: a body over MaxBody is clipped and a TTL
// over MaxTTL is clamped by the adapter that sends them, because a too-long
// notice is still a notice and refusing it is refusing to tell somebody.
func (m Message) Validate() error {
	if m.Title == "" && m.Body == "" {
		return fmt.Errorf("%w: a message with neither a title nor a body tells nobody anything", ErrInvalid)
	}
	if len(m.Tag) > MaxTag {
		return fmt.Errorf("%w: a tag of %d bytes is longer than %d", ErrInvalid, len(m.Tag), MaxTag)
	}
	if m.TTL < 0 {
		return fmt.Errorf("%w: a TTL of %s asks a provider to try backwards in time", ErrInvalid, m.TTL)
	}
	if m.URL != "" {
		if _, err := url.Parse(m.URL); err != nil {
			return fmt.Errorf("%w: the link %q is not a URL: %v", ErrInvalid, m.URL, err)
		}
	}
	return nil
}

// TTLSeconds is a message's lifetime as a provider is told it, in whole seconds:
// the message's own TTL, or the adapter's configured ceiling for a message that
// names none, or MaxTTL where the deployment configured no ceiling either. A
// configured ceiling bounds the send in both directions — a deployment that says
// "an hour" is not overruled by a message asking for a day — and the port's own
// MaxTTL bounds the ceiling. The result is rounded up and never zero: both
// platforms drop a message at zero on a sleeping device instead of delivering it
// late, and a part-second of tolerance is a second on the wire.
func TTLSeconds(m Message, ceiling time.Duration) int64 {
	ttl := m.TTL
	if ttl <= 0 {
		ttl = ceiling
	}
	if ttl <= 0 {
		ttl = MaxTTL
	}
	if ceiling > 0 && ttl > ceiling {
		ttl = ceiling
	}
	if ttl > MaxTTL {
		ttl = MaxTTL
	}
	seconds := int64(ttl / time.Second)
	if ttl%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	return seconds
}

// State is what the party we talked to promised.
type State string

const (
	// Delivered is "it took the message for delivery and gives no further
	// answer" — a Web Push endpoint's 201.
	Delivered State = "delivered"
	// Accepted is "it took the message; the outcome comes later", under
	// Receipt.ID.
	Accepted State = "accepted"
)

// Receipt is one adapter's answer about one send. It carries no device and no
// token: the caller addressed the device, so echoing it back would be a second
// place a token can leak from.
type Receipt struct {
	State State
	// ID is the provider's own id for the send — Expo's ticket id — and "" when
	// the platform answers with none.
	ID string
}

// Adapter carries the devices of one platform.
//
// Validate is on the port, and not merely inside the adapters, because the only
// party that can say whether a token is carryable is the carrier of it, and a
// store that wrote garbage would fail every later notice to that person forever.
// It is a syntax check and asks no provider anything: a well-formed token nobody
// has used yet is written today and answered ErrGone later.
type Adapter interface {
	Platform() Platform
	Validate(token string) error
	Send(ctx context.Context, d Device, m Message) (Receipt, error)
}

// Adapters is the composition's answer to "what does this installation carry",
// one adapter per platform — the same shape, and the same Get/Active duty, as
// notification/contracts.Providers. A key wired to a nil adapter is not a
// platform, whichever way it is asked.
type Adapters map[Platform]Adapter

// Get is the adapter for a platform, or nil.
func (a Adapters) Get(p Platform) Adapter { return a[p] }

// Active is the platforms this installation carries — every key with an adapter
// behind it, in Platforms' order — so a route that lists what is reachable and a
// router that refuses what is not ask one question and get one answer.
func (a Adapters) Active() []Platform {
	out := make([]Platform, 0, len(a))
	for _, p := range Platforms {
		if a[p] != nil {
			out = append(out, p)
		}
	}
	return out
}

// Send routes one message to the adapter of the device's platform and opens the
// one span every send owes, which is why callers route here rather than through
// the map: an adapter's own HTTP call is then a child of a span that names the
// platform, the tenant and the outcome, and a trace of one notice reads request
// to deliver to send. A caller that takes an Adapter out of the map and calls it
// directly owes itself a span.
func (a Adapters) Send(ctx context.Context, d Device, m Message) (Receipt, error) {
	ctx, span := telemetry.Tracer().Start(ctx, "push send")
	defer span.End()
	span.SetAttributes(
		attribute.String("push.platform", string(d.Platform)),
		attribute.String("push.tenant", d.TenantID.String()),
		attribute.String("push.device", d.ID.String()),
	)

	adapter := a.Get(d.Platform)
	if adapter == nil {
		span.SetAttributes(attribute.String("push.error", "ErrUnsupportedPlatform"))
		return Receipt{}, fmt.Errorf("%w: %s", ErrUnsupportedPlatform, d.Platform)
	}
	if m.Body != m.Clip().Body {
		span.SetAttributes(attribute.Bool("push.truncated", true))
	}

	receipt, err := adapter.Send(ctx, d, m)
	if err != nil {
		span.SetAttributes(attribute.String("push.error", errorName(err)))
		span.RecordError(err)
		return Receipt{}, err
	}
	span.SetAttributes(attribute.String("push.state", string(receipt.State)))
	return receipt, nil
}

// errorName is the sentinel's name for a span attribute. A span never carries
// the error text a provider wrote, because the sentence a provider writes about
// a token can contain the token, and a trace is read by more people than hold
// the database.
func errorName(err error) string {
	switch {
	case errors.Is(err, ErrGone):
		return "ErrGone"
	case errors.Is(err, ErrMalformed):
		return "ErrMalformed"
	case errors.Is(err, ErrRejected):
		return "ErrRejected"
	case errors.Is(err, ErrInvalid):
		return "ErrInvalid"
	case errors.Is(err, ErrUnsupportedPlatform):
		return "ErrUnsupportedPlatform"
	default:
		return "transient"
	}
}
