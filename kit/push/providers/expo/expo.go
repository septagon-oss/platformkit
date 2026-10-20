// Package expo carries push messages to Expo's push HTTP API, which reaches the
// FCM and APNs services behind it.
//
// The API is a JSON endpoint, so this adapter is net/http and encoding/json and
// no SDK: a dependency would buy a name a reader has to look up in order to
// understand one POST, and Expo's own reference calls sending requests directly
// to the HTTP API a supported path.
//
// The token grammar is deliberately loose. Expo's push tokens have changed
// spelling between SDK versions, and a pattern strict enough to reject a token
// it does not recognise is a pattern that deletes people's devices on an SDK
// upgrade. What is refused here is what could not be a token at all — nothing, a
// page of text, a blob — and everything else is asked of Expo, whose answer
// about a token nobody has registered arrives as a gone token.
package expo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/septagon-oss/platformkit/kit/push"
)

// MaxToken is the longest string this adapter will put in a request. It is a
// shape limit rather than a protocol one: a real Expo token is a few dozen
// characters, and what reaches this length is a paste of the wrong thing.
const MaxToken = 1024

// sendPath is Expo's own endpoint for sending, from its reference on sending
// notifications.
const sendPath = "/--/api/v2/push/send"

// maxNotification is Expo's ceiling on one notification payload, counted over
// the JSON this package posts. Expo's reference names the error a longer one
// gets (MessageTooBig) rather than the bytes it allows, and the count it uses is
// of the notification, so the recipient token is included here to spend: a
// notice that fits this number fits whichever way the far end counts.
const maxNotification = 4096

// Config is what a deployment knows that this package cannot.
type Config struct {
	// BaseURL is Expo's host, defaulting to https://exp.host. A test points it
	// at a stand-in; a proxy is the other thing a deployment sets it to.
	BaseURL string
	// AccessToken is the project's push credential, sent as a bearer token when
	// set. Expo's HTTP API needs none today, and a per-tenant push key is the
	// product's decision, made by building one Config per tenant.
	AccessToken string
	// HTTPClient is nil for the adapter's own, which is a plain one with a
	// 10-second ceiling: a worker's transaction holds the connection open
	// otherwise.
	HTTPClient *http.Client
	// TTL is how long Expo may keep trying: the ceiling for every send this
	// adapter makes, zero meaning push.MaxTTL. Expo's own default runs to four
	// weeks and a message that asks for longer is shortened to this; a message
	// that asks for less keeps its own.
	TTL time.Duration
}

// New is this adapter as the composition wants it: Expo alone.
func New(cfg Config) push.Adapters {
	return push.Adapters{push.PlatformExpo: &adapter{cfg: newClient(cfg)}}
}

type client struct {
	baseURL string
	token   string
	http    *http.Client
	ttl     time.Duration
}

func newClient(cfg Config) *client {
	c := &client{
		baseURL: strings.TrimSuffix(cfg.BaseURL, "/"),
		token:   cfg.AccessToken,
		http:    cfg.HTTPClient,
		ttl:     cfg.TTL,
	}
	if c.baseURL == "" {
		c.baseURL = "https://exp.host"
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 10 * time.Second}
	}
	return c
}

type message struct {
	To         string         `json:"to"`
	Title      string         `json:"title,omitempty"`
	Body       string         `json:"body,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	CollapseID string         `json:"collapseId,omitempty"`
	TTL        int64          `json:"ttl"`
}

// ticket is Expo's answer about one send. A single message answers with one
// ticket object and an array answers with one per message, so both shapes read
// through the same field.
type ticket struct {
	Status  string `json:"status"`
	ID      string `json:"id"`
	Message string `json:"message"`
	Details struct {
		Error string `json:"error"`
	} `json:"details"`
}

type response struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// deviceNotRegistered is Expo's own spelling, in the ticket and in the receipt
// alike, of "stop sending to this token until it registers again". It is the
// provider's sentence, not ours, and the case in pushtest pins it: an adapter
// that stops matching it silently keeps every dead token in the store.
const deviceNotRegistered = "DeviceNotRegistered"

type adapter struct{ cfg *client }

func (a *adapter) Platform() push.Platform { return push.PlatformExpo }

// Validate refuses what cannot be a token: empty, longer than any token is, or
// holding anything but printable ASCII — a token is one opaque word, so
// whitespace is a paste of prose and a brace or a quote is a value somebody
// serialised, which is the shape of another platform's credential and not of
// this one's. It does not check the spelling.
func (a *adapter) Validate(token string) error {
	if token == "" {
		return fmt.Errorf("%w: an empty token addresses no device", push.ErrMalformed)
	}
	if len(token) > MaxToken {
		return fmt.Errorf("%w: a token of %d bytes is longer than any Expo push token", push.ErrMalformed, len(token))
	}
	if strings.ContainsAny(token, " \t\n\r") {
		return fmt.Errorf("%w: a token with whitespace in it was copied out of something else", push.ErrMalformed)
	}
	for _, r := range token {
		if r < 0x20 || r >= 0x7f {
			return fmt.Errorf("%w: a token with %q in it is not one Expo hands out", push.ErrMalformed, r)
		}
		if strings.ContainsRune(`{}"\`, r) {
			return fmt.Errorf("%w: a token written as %q is a serialised value, not a token", push.ErrMalformed, r)
		}
	}
	return nil
}

func (a *adapter) Send(ctx context.Context, d push.Device, m push.Message) (push.Receipt, error) {
	receipt, err := a.send(ctx, d, m)
	// Expo answers a refusal with a sentence about what it was refused, and what
	// it was refused is a device token, so nothing this adapter returns goes out
	// before the token — and the project's own push credential, in case a relay
	// echoes the request it was given — has been cut out of it.
	return receipt, push.Redact(err, d.Token, a.cfg.token)
}

func (a *adapter) send(ctx context.Context, d push.Device, m push.Message) (push.Receipt, error) {
	if d.Platform != push.PlatformExpo {
		return push.Receipt{}, fmt.Errorf("%w: the Expo adapter carries no device of %s", push.ErrInvalid, d.Platform)
	}
	if err := m.Validate(); err != nil {
		return push.Receipt{}, err
	}
	if err := a.Validate(d.Token); err != nil {
		return push.Receipt{}, err
	}
	m = m.Clip()
	body, err := a.request(d.Token, m)
	if err != nil {
		return push.Receipt{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.baseURL+sendPath, bytes.NewReader(body))
	if err != nil {
		return push.Receipt{}, fmt.Errorf("%w: %s is not a URL this adapter can post to: %v", push.ErrInvalid, a.cfg.baseURL, err)
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/json")
	if a.cfg.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.cfg.token)
	}

	resp, err := a.cfg.http.Do(req)
	if err != nil {
		// A dropped connection or a timeout says nothing about the device, so it
		// is returned bare: the caller's transaction rolls back and the outbox
		// retries. This is the mapping the gone token depends on.
		return push.Receipt{}, fmt.Errorf("expo: send: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
	}()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return push.Receipt{}, fmt.Errorf("expo: read the answer: %w", err)
	}
	return answer(resp.StatusCode, raw, d.Token, a.cfg.token)
}

// request is the POST body, shortened until it fits one notification: a notice
// whose copy is too long for the platform arrives shortly and partly rather than
// not at all, which is the same choice the port makes at MaxBody. The body comes
// back encoded because the length that decides the shortening is the length of
// what goes on the wire.
func (a *adapter) request(token string, m push.Message) ([]byte, error) {
	for range 4 {
		msg := message{To: token, Title: m.Title, Body: m.Body, CollapseID: m.Tag, TTL: push.TTLSeconds(m, a.cfg.ttl)}
		if m.URL != "" {
			msg.Data = map[string]any{"link": m.URL}
		}
		body, err := json.Marshal(msg)
		if err != nil {
			return nil, fmt.Errorf("expo: encode the message: %w", err)
		}
		if len(body) <= maxNotification {
			return body, nil
		}
		m = m.TrimTo(len(body) - maxNotification)
	}
	return nil, fmt.Errorf("%w: an Expo notice titled in %d bytes and linked in %d does not fit %d",
		push.ErrInvalid, len(m.Title), len(m.URL), maxNotification)
}

// answer is the mapping from one HTTP exchange to the port's vocabulary, and it
// is the whole of this adapter's judgement: a 400 or a 413 refuses the message as
// written and keeps the device; a ticket naming DeviceNotRegistered ends the
// device; a 429, a 5xx or anything else the transport did is nobody's refusal and
// is returned bare.
//
// secrets are this send's own credentials — the device's token and the project's
// push key — which is what the provider echoes back in the sentence below, so
// they are cut out of it here rather than left to the caller's redaction: an
// answer that has already been shortened has nothing left for a caller to match.
func answer(status int, raw []byte, secrets ...string) (push.Receipt, error) {
	// A 400 or a 413 is Expo refusing what was posted rather than the device it
	// was posted for, and every other status is the service itself: the sentence
	// goes to the caller bare, so the outbox retries it.
	if status == http.StatusBadRequest || status == http.StatusRequestEntityTooLarge {
		return push.Receipt{}, fmt.Errorf("%w: expo refused the request: %s", push.ErrRejected, reason(raw, secrets...))
	}
	if status != http.StatusOK {
		return push.Receipt{}, fmt.Errorf("expo: the push service answered %d: %s", status, reason(raw, secrets...))
	}
	var got response
	if err := json.Unmarshal(raw, &got); err != nil {
		return push.Receipt{}, fmt.Errorf("expo: read the ticket: %w", err)
	}
	if len(got.Errors) > 0 && len(got.Data) == 0 {
		return push.Receipt{}, fmt.Errorf("%w: expo refused the request: %s", push.ErrRejected, got.Errors[0].Message)
	}
	var t ticket
	if err := json.Unmarshal([]byte(got.Data), &t); err != nil {
		return push.Receipt{}, fmt.Errorf("expo: read the ticket: %w", err)
	}
	switch t.Status {
	case "ok":
		return push.Receipt{State: push.Accepted, ID: t.ID}, nil
	case "error":
		if t.Details.Error == deviceNotRegistered {
			return push.Receipt{}, push.Gone("expo push ticket: " + deviceNotRegistered)
		}
		return push.Receipt{}, fmt.Errorf("%w: %s", push.ErrRejected, t.Message)
	default:
		return push.Receipt{}, fmt.Errorf("expo: the push service answered a ticket in the state %q", t.Status)
	}
}

// reason is the body as far as an operator needs it: a sentence for the ledger,
// with the send's credentials cut out of it and then the sentence cut short — in
// that order, which is the whole point. Truncation first would leave the front of
// a long token or key standing where the caller's exact-string redaction can no
// longer reach it, and a trace event or a ledger reason would then carry a
// credential's best half. Never a header.
func reason(raw []byte, secrets ...string) string {
	s := strings.TrimSpace(push.Scrub(string(raw), secrets...))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
