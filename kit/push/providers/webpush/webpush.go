// Package webpush carries push messages to a browser subscription over Web Push
// with VAPID authentication: RFC 8030 for the request and its answers, RFC 8291
// for the encryption, RFC 8292 for the credentials.
//
// The encryption and the JWT live in the library this package imports; what
// lives here is the port's judgement — which answer means the subscription is
// over, which one means the message was refused, and which one means nothing at
// all about the device.
//
// Device.Token for this platform is the browser's serialised PushSubscription,
// the JSON a service worker hands out, and this package alone knows that. It is
// why a send posts to a URL that arrived inside a stored token, and why an
// adapter can be driven against a stand-in on loopback with no seam in it.
package webpush

import (
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	webpushlib "github.com/SherClockHolmes/webpush-go"

	"github.com/septagon-oss/platformkit/kit/push"
)

// MaxToken is the longest serialised subscription this adapter reads. A browser
// subscription is a URL and two short keys; the ceiling is where a pasted blob
// stops looking like one.
const MaxToken = 8192

// The two lengths a subscription key has. RFC 8291 §5 hands over an
// authentication secret of at least 16 octets and the user agent's public key in
// the uncompressed SEC1 form, which for P-256 is one 0x04 prefix and two 32-byte
// coordinates. A subscription whose keys are neither cannot be encrypted for, so
// naming the length here is what makes a stored-but-uncarryable token a
// registration refusal rather than a send failure.
const (
	minAuthSecret = 16
	publicKeySize = 65
)

// maxTopic is RFC 8030 §5.4's ceiling on a Topic header and topicSafe is its
// grammar: one to 32 characters of URL-safe base64 without padding. A push
// service answers 400 for anything else, and a request refused that way loses
// the message.
const maxTopic = 32

var topicSafe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// Config is what a deployment knows that this package cannot.
type Config struct {
	// PublicKey and PrivateKey are the VAPID P-256 pair as the library reads it:
	// base64url of the uncompressed public point and of the private scalar,
	// unpadded — what webpush-go's own generator returns, not PEM. They come from
	// the deployment's secret store and never from a column: a push token re-sends
	// a notification, and a private key sends one to anybody's phone.
	PublicKey, PrivateKey string
	// Subscriber is the contact RFC 8030 asks a push service to be able to
	// reach, in the form mailto:push@example.test.
	Subscriber string
	// HTTPClient is nil for the adapter's own, a plain client with a 10-second
	// ceiling. A test injects its stand-in's.
	HTTPClient *http.Client
	// TTL is how long the push service may keep trying: the ceiling for every send
	// this adapter makes, zero meaning push.MaxTTL. A message that asks for longer
	// is shortened to it, and a message that asks for less keeps its own.
	TTL time.Duration
	// RecordSize is the content-coding record size, zero for the library's own
	// 4096, which is RFC 8030's ceiling.
	RecordSize uint32
}

// New is this adapter as the composition wants it: the web platform alone.
func New(cfg Config) push.Adapters {
	return push.Adapters{push.PlatformWeb: &adapter{cfg: cfg}}
}

type adapter struct{ cfg Config }

func (a *adapter) Platform() push.Platform { return push.PlatformWeb }

// subscription is the stored token. The library's own type is not what a device
// row holds, so the shape is read here: an unknown field is ignored, and a
// subscription missing either key is refused below rather than half-carried.
type subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		Auth   string `json:"auth"`
		P256dh string `json:"p256dh"`
	} `json:"keys"`
}

// Validate refuses anything that is not a subscription a browser handed out: too
// long to be one, not a JSON object, an endpoint that is not an https URL (a
// browser only ever hands out an https endpoint, so anything else was typed), or
// a key that could not be encrypted with — missing, not base64 in either
// spelling the library reads, of a length no browser's key has, or a public point
// that is not on the curve the encryption happens on.
func (a *adapter) Validate(token string) error {
	if token == "" {
		return fmt.Errorf("%w: an empty subscription addresses no device", push.ErrMalformed)
	}
	if len(token) > MaxToken {
		return fmt.Errorf("%w: a subscription of %d bytes is longer than any browser hands out", push.ErrMalformed, len(token))
	}
	sub, err := decode(token)
	if err != nil {
		return err
	}
	if sub.Keys.P256dh == "" || sub.Keys.Auth == "" {
		return fmt.Errorf("%w: a subscription without both keys cannot be encrypted for", push.ErrMalformed)
	}
	if raw, err := subscriptionKey(sub.Keys.Auth); err != nil {
		return keyRefused("auth", err)
	} else if len(raw) < minAuthSecret {
		return keyWrongSize("auth", len(raw))
	}
	raw, err := subscriptionKey(sub.Keys.P256dh)
	if err != nil {
		return keyRefused("p256dh", err)
	}
	// The uncompressed SEC1 form of a P-256 point: one 0x04 and two coordinates.
	if len(raw) != publicKeySize || raw[0] != 0x04 {
		return keyWrongSize("p256dh", len(raw))
	}
	// Shape is not substance. Sixty-five bytes with the right prefix can still
	// name no key: an x that is not on P-256 has no shared secret with anybody,
	// and a push service cannot decrypt what was encrypted for it. The sender
	// this package sends with discovers that mid-send, in its own words, which is
	// a retryable-looking failure for input no retry can improve — so the point
	// is checked here, where a unusable subscription is still malformed input.
	if _, err := ecdh.P256().NewPublicKey(raw); err != nil {
		return keyUnusable("p256dh", err)
	}
	return nil
}

// subscriptionKey reads one subscription key as the library this package sends
// with reads it — padded or unpadded, standard or URL-safe alphabet. The spelling
// is that library's own decodeSubscriptionKey, so a key accepted here is a key
// that encrypts and one refused here would be an error at send time the caller
// would retry until its ladder gave up.
func subscriptionKey(value string) ([]byte, error) {
	padded := value
	if rem := len(padded) % 4; rem != 0 {
		padded += strings.Repeat("=", 4-rem)
	}
	raw, err := base64.StdEncoding.DecodeString(padded)
	if err == nil {
		return raw, nil
	}
	return base64.URLEncoding.DecodeString(padded)
}

func keyRefused(field string, err error) error {
	return fmt.Errorf("%w: the subscription's %s is not base64: %v", push.ErrMalformed, field, err)
}

func keyWrongSize(field string, got int) error {
	return fmt.Errorf("%w: the subscription's %s decodes to %d bytes, which is no browser's key", push.ErrMalformed, field, got)
}

func keyUnusable(field string, err error) error {
	return fmt.Errorf("%w: the subscription's %s is not a key anything can be encrypted for: %v", push.ErrMalformed, field, err)
}

func decode(token string) (*subscription, error) {
	var sub subscription
	if err := json.Unmarshal([]byte(token), &sub); err != nil {
		return nil, push.Redact(fmt.Errorf("%w: the stored subscription is not JSON: %v", push.ErrMalformed, err), token)
	}
	u, err := url.Parse(sub.Endpoint)
	if err != nil {
		// The parse error quotes the URL it failed on, and for this platform the
		// URL is half of what it takes to push to somebody.
		return nil, push.Redact(fmt.Errorf("%w: the subscription's endpoint is not a URL: %v", push.ErrMalformed, err), sub.Endpoint, token)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("%w: a push endpoint at %q is not one a browser hands out", push.ErrMalformed, u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%w: the subscription's endpoint names no host", push.ErrMalformed)
	}
	return &sub, nil
}

func (a *adapter) Send(ctx context.Context, d push.Device, m push.Message) (push.Receipt, error) {
	if d.Platform != push.PlatformWeb {
		return push.Receipt{}, fmt.Errorf("%w: the web push adapter carries no device of %s", push.ErrInvalid, d.Platform)
	}
	if err := m.Validate(); err != nil {
		return push.Receipt{}, err
	}
	sub, err := decode(d.Token)
	if err != nil {
		return push.Receipt{}, err
	}
	if err := a.Validate(d.Token); err != nil {
		return push.Receipt{}, err
	}
	m = m.Clip()
	body, m, err := payload(m)
	if err != nil {
		return push.Receipt{}, err
	}

	resp, err := webpushlib.SendNotificationWithContext(ctx, body, &webpushlib.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpushlib.Keys{Auth: sub.Keys.Auth, P256dh: sub.Keys.P256dh},
	}, &webpushlib.Options{
		HTTPClient:      a.client(),
		RecordSize:      a.cfg.RecordSize,
		Subscriber:      a.cfg.Subscriber,
		Topic:           topic(m.Tag),
		TTL:             int(push.TTLSeconds(m, a.cfg.TTL)),
		VAPIDPublicKey:  a.cfg.PublicKey,
		VAPIDPrivateKey: a.cfg.PrivateKey,
	})
	if err != nil {
		// Nothing about the device is learned from a failed exchange, so the error
		// goes back for the caller to retry — but the transport's own sentence names
		// the URL it failed to reach, and that URL is half of what it takes to push
		// to this person.
		return push.Receipt{}, push.Redact(fmt.Errorf("webpush: send: %w", err), sub.Endpoint, sub.Keys.Auth, sub.Keys.P256dh)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		return push.Receipt{State: push.Delivered, ID: resp.Header.Get("Location")}, nil
	case http.StatusNotFound, http.StatusGone:
		// RFC 8030 §5: the push service no longer knows this subscription, in
		// which case the row that holds it is a lie about a device.
		return push.Receipt{}, push.Gone(fmt.Sprintf("webpush %d", resp.StatusCode))
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge:
		// The subscription is unusable as stored, but nothing about it says the
		// person no longer has the browser: the client re-registers, the row stays.
		return push.Receipt{}, fmt.Errorf("%w: the push service refused the subscription at %d", push.ErrRejected, resp.StatusCode)
	default:
		return push.Receipt{}, fmt.Errorf("webpush: the push service answered %d", resp.StatusCode)
	}
}

// payload is the message as the service worker receives it: JSON rather than the
// plain text RFC 8030 also permits, because a worker reads event.data.json() and a
// text payload would lose the deep link and the tag it collapses on — and short
// enough for one record, which is a tighter bound than the port's MaxBody, since
// the whole envelope (the title, the link, the JSON around the body) travels
// inside the same 4096 bytes. The message comes back as it was shortened, so the
// caller can see the copy that went.
func payload(m push.Message) ([]byte, push.Message, error) {
	for attempt := 0; attempt < 4; attempt++ {
		body, err := json.Marshal(struct {
			Title string `json:"title"`
			Body  string `json:"body"`
			URL   string `json:"url,omitempty"`
			Tag   string `json:"tag,omitempty"`
		}{Title: m.Title, Body: m.Body, URL: m.URL, Tag: m.Tag})
		if err != nil {
			return nil, m, fmt.Errorf("webpush: encode the message: %w", err)
		}
		if len(body) <= maxPayload {
			return body, m, nil
		}
		m = m.TrimTo(len(body) - maxPayload)
	}
	return nil, m, fmt.Errorf("%w: a title of %d bytes and a link of %d do not fit one push record",
		push.ErrInvalid, len(m.Title), len(m.URL))
}

// topic is the message's collapse key as this protocol can carry one. A key the
// protocol accepts goes out as it stands, because it is readable in a push
// service's own log; anything longer goes out as a digest of itself, because
// RFC 8030 §5.4 caps a Topic at 32 URL-safe base64 characters and answers 400 to
// a longer one, which would lose the message. The digest is stable, so a retry
// collapses on the same notification — and the notification id this module sends
// is a UUID, 36 characters, so the digest is the normal answer rather than the
// rare one.
func topic(tag string) string {
	switch {
	case tag == "":
		return ""
	case topicSafe.MatchString(tag):
		return tag
	default:
		sum := sha256.Sum256([]byte(tag))
		return base64.RawURLEncoding.EncodeToString(sum[:])[:maxTopic]
	}
}

// maxPayload is the largest envelope one record can carry: RFC 8030's ceiling
// less the room the content coding takes. A record holds the payload *and* its
// encryption — a content-coding header, a salt, a local public key and the
// padding the coding adds — so the copy that fits is shorter than the 4096 the
// record is capped at, and the number is checked here rather than left to the
// library's own refusal so that a notice short enough to send is never one
// refused on the way out.
const maxPayload = int(webpushlib.MaxRecordSize) - 512

func (a *adapter) client() *http.Client {
	if a.cfg.HTTPClient != nil {
		return a.cfg.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}
