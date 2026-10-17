// Package webpush carries push messages to a browser subscription over Web Push
// with VAPID authentication: RFC 8030 for the request and its answers, RFC 8291
// for the encryption, RFC 8292 for the credentials.
//
// The encryption and the JWT live in the library this package imports; what
// lives here is the port's judgement — which answer means the subscription is
// over, which one means the message was refused, and which one means nothing at
// all about the device.
//
// Device.Token for this platform is the browser's serialised PushSubscription:
// the JSON a service worker hands out as `{"endpoint":"https://…",
// "keys":{"auth":"…","p256dh":"…"}}`. This package, and only this package,
// knows that. It is also why a send posts to a URL that arrived inside the
// stored token, and why an adapter can be driven against a stand-in over the
// loopback interface with no seam in the library at all.
package webpush

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
	"unicode/utf8"

	webpushlib "github.com/SherClockHolmes/webpush-go"

	"github.com/septagon-oss/platformkit/kit/push"
)

// MaxToken is the longest serialised subscription this adapter reads. A browser
// subscription is a URL and two short keys; the ceiling is where a pasted blob
// stops looking like one.
const MaxToken = 8192

// Config is what a deployment knows that this package cannot.
type Config struct {
	// PublicKey and PrivateKey are the VAPID P-256 pair in PEM. They come from
	// the deployment's secret store and never from a column: a push token
	// re-sends a notification, and a private key sends one to anybody's phone.
	PublicKey, PrivateKey string
	// Subscriber is the contact RFC 8030 asks a push service to be able to
	// reach, in the form mailto:push@example.test.
	Subscriber string
	// HTTPClient is nil for the adapter's own, a plain client with a 10-second
	// ceiling. A test injects its stand-in's.
	HTTPClient *http.Client
	// TTL is how long the push service may keep trying, zero meaning
	// push.MaxTTL.
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
// a missing key, without which the payload could not be encrypted for anybody.
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
	return nil
}

func decode(token string) (*subscription, error) {
	var sub subscription
	if err := json.Unmarshal([]byte(token), &sub); err != nil {
		return nil, fmt.Errorf("%w: the stored subscription is not JSON: %v", push.ErrMalformed, err)
	}
	u, err := url.Parse(sub.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: the subscription's endpoint is not a URL: %v", push.ErrMalformed, err)
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
		Topic:           m.Tag,
		TTL:             int(ttlSeconds(m)),
		VAPIDPublicKey:  a.cfg.PublicKey,
		VAPIDPrivateKey: a.cfg.PrivateKey,
	})
	if err != nil {
		// Nothing about the device is learned from a failed exchange, so the
		// error goes back bare and the caller retries.
		return push.Receipt{}, fmt.Errorf("webpush: send: %w", err)
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
		m = trim(m, len(body)-maxPayload)
	}
	return nil, m, fmt.Errorf("%w: a title of %d bytes and a link of %d do not fit one push record",
		push.ErrInvalid, len(m.Title), len(m.URL))
}

// trim shortens a body by n bytes at a rune boundary, to nothing at worst.
func trim(m push.Message, n int) push.Message {
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

// ttlSeconds is the message's TTL in whole seconds, clamped to the port's
// ceiling and rounded up, and never zero: a TTL of zero tells the push service to
// drop the message rather than deliver it late.
func ttlSeconds(m push.Message) int64 {
	ttl := m.TTL
	if ttl <= 0 {
		ttl = push.MaxTTL
	}
	if ttl > push.MaxTTL {
		ttl = push.MaxTTL
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
