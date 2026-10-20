// Package log is the push adapter for a deployment that carries messages to a
// log line: the development composition, the test that wants to know a notice
// reached the transport, and a production installation with no push credentials
// yet. It opens no socket.
//
// One recorder stands behind every platform the port names, because this
// adapter is the composition's stand-in and a development deployment has to
// carry whatever a client registers — a phone token and a browser subscription
// both arriving at a machine that speaks neither. It is not a simulation of a
// push service: it answers Delivered because it did what it does, which was to
// record the send, and it never reports a token gone, so a test that wants the
// gone path wires a stand-in that answers it.
package log

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/septagon-oss/platformkit/kit/push"
)

// MaxToken is the longest token this recorder accepts, the same ceiling the
// device table gives a token. A blob pasted into a token field is refused here
// as it is refused by every real adapter, so a development machine cannot accept
// a row production would refuse.
const MaxToken = 8192

// Recorded is one send: the message, and the device it was addressed to.
type Recorded struct {
	Device  push.Device
	Message push.Message
}

// Hub is the recorder and the adapters that write to it.
type Hub struct {
	mu   sync.Mutex
	sent []Recorded

	// Logger is where a send is written; nil is slog.Default(). A test sets it
	// to a handler of its own to read the lines.
	Logger *slog.Logger
}

// NewHub is an empty recorder.
func NewHub() *Hub { return &Hub{} }

// New is the adapters of a recorder nobody reads back, for a composition that
// only wants the log line.
func New() push.Adapters { return NewHub().Adapters() }

// Adapters is one adapter per platform the port names, all writing to this
// recorder, so a development deployment carries whatever a client registers.
func (h *Hub) Adapters() push.Adapters {
	out := make(push.Adapters, len(push.Platforms))
	for _, p := range push.Platforms {
		out[p] = &adapter{hub: h, platform: p}
	}
	return out
}

// Sent is every send this recorder holds, in the order they arrived — the unit
// a module test reads: which notices reached the transport, and which device each
// was addressed to.
func (h *Hub) Sent() []Recorded {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Recorded(nil), h.sent...)
}

func (h *Hub) record(r Recorded) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sent = append(h.sent, r)
}

func (h *Hub) log(ctx context.Context, args ...any) {
	l := h.Logger
	if l == nil {
		l = slog.Default()
	}
	l.InfoContext(ctx, "push: the message was carried to the log", args...)
}

// adapter is one platform's view of the recorder.
type adapter struct {
	hub      *Hub
	platform push.Platform
}

func (a *adapter) Platform() push.Platform { return a.platform }

// Validate refuses what no client could ever hand out: nothing at all, a blob,
// or a token with a control character in it. It deliberately accepts any shape
// otherwise, because this adapter stands for platforms whose token grammar is
// not its own, and refusing an Expo token "for a web device" here would mean the
// development machine refuses what production carries.
func (a *adapter) Validate(token string) error {
	if token == "" {
		return fmt.Errorf("%w: an empty token addresses no device", push.ErrMalformed)
	}
	if len(token) > MaxToken {
		return fmt.Errorf("%w: a token of %d bytes is longer than %d", push.ErrMalformed, len(token), MaxToken)
	}
	for _, r := range token {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: a token with a control character in it was not handed out by a client", push.ErrMalformed)
		}
	}
	return nil
}

func (a *adapter) Send(ctx context.Context, d push.Device, m push.Message) (push.Receipt, error) {
	if d.Platform != a.platform {
		return push.Receipt{}, fmt.Errorf("%w: this adapter carries %s, not a device of %s",
			push.ErrInvalid, a.platform, d.Platform)
	}
	if err := m.Validate(); err != nil {
		return push.Receipt{}, err
	}
	if err := a.Validate(d.Token); err != nil {
		return push.Receipt{}, err
	}
	m = m.Clip()
	a.hub.record(Recorded{Device: d, Message: m})
	a.hub.log(ctx,
		"device", d.ID, "platform", string(a.platform), "recipient", d.UserID,
		"title", m.Title, "token", redact(d.Token))
	return push.Receipt{State: push.Delivered}, nil
}

// redact is the token as this adapter writes it: enough to recognise a device
// across two log lines, too little to push to. A full token in a log file is a
// credential anyone who has the file could send a notification with.
func redact(token string) string {
	r := []rune(token)
	if len(r) <= 6 {
		return string(r) + "…"
	}
	return string(r[:6]) + "…"
}
