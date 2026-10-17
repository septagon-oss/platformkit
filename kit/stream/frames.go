package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/module"
)

// The frame format is HTML Living Standard §9.2.7 — the Server-Sent Events stream
// format — written by hand because nothing here adds a dependency it does not need.
// The standard's own rules are followed rather than approximated: every frame ends
// in a blank line, a field is "name: value" with one space, and a value never
// contains a newline. The only caller-supplied string ever written is a topic, and
// a topic is an event name, which transport.ValidName already restricts.
//
// What the standard offers that this delivery declines is the resume: no `id` is
// written on a frame that is not a notice, so the position a browser would send
// back in Last-Event-ID is a position this endpoint never claimed. A connection that
// cannot promise it missed nothing is told so and refetches.
const (
	// opening is the frame that begins every connection: the reconnect delay, so
	// the number is not something a client has to remember, and a comment that says
	// what this stream is to anybody reading a raw socket.
	opening = "retry: 3000\n: pkit stream\n\n"
	// ping is the heartbeat's comment. It reaches no handler in the browser; it
	// exists so a proxy does not decide this connection is dead.
	ping = ": ping\n\n"
	// closing is the last thing written to a connection whose session ended.
	closing = ": closing\n\n"
)

// The three reasons a connection is told it is not current, or is told a topic is
// gone. A reason is a machine token: a frame is not a refusal, so nothing in one is
// translatable, and the translated sentence belongs to the problem document.
const (
	reasonConnect    = "connect"
	reasonOverflow   = "overflow"
	reasonPermission = "permission"
)

// errNoTopicsLeft ends a connection whose last topic was withdrawn; errSessionEnded
// ends one whose caller no longer has a session. Neither is a refusal: the response
// is already on the wire, so there is no answer left to write.
var (
	errNoTopicsLeft = errors.New("stream: every topic of this connection was withdrawn")
	errSessionEnded = errors.New("stream: this caller's session has ended")
)

// noticeData is a notice's body: when the state changed, and nothing else. No
// payload member appears in a frame — a notice is a hint to read, not a copy.
type noticeData struct {
	Time string `json:"time"`
}

// resyncData is the frame that says what the connection holds is not current.
// connect is what every connection is told first, because a socket opened now has
// by definition missed everything before now; overflow is the loss of notices this
// connection could not read fast enough.
type resyncData struct {
	Time   string `json:"time"`
	Reason string `json:"reason"`
}

// droppedData is a topic taken away from a live connection, and why.
type droppedData struct {
	Topic  string `json:"topic"`
	Reason string `json:"reason"`
}

// frame is one event waiting for the wire.
type frame struct {
	// id is a notice's event id, and uuid.Nil for every frame that carries no
	// position a server would have to honour.
	id    uuid.UUID
	event string
	data  any
}

func (f frame) bytes() []byte {
	var out []byte
	if f.id != uuid.Nil {
		out = append(out, "id: "...)
		out = append(out, f.id.String()...)
		out = append(out, '\n')
	}
	out = append(out, "event: "...)
	out = append(out, f.event...)
	out = append(out, '\n')
	body, err := json.Marshal(f.data)
	if err != nil {
		// Every member is a string this package wrote, so a failure is a bug and
		// not a condition. Say so in the frame rather than sending a broken one.
		body = []byte(`{"time":""}`)
	}
	out = append(out, "data: "...)
	out = append(out, body...)
	out = append(out, '\n', '\n')
	return out
}

func (h *Hub) stamp() string { return stamp(h.clock.Now()) }

func stamp(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }

// open writes the headers, the opening comment and the first resync, and flushes
// them: until this returns nothing of the response has reached the wire, which is
// what makes every refusal above it an answer rather than an event.
func (h *Hub) open(w http.ResponseWriter, rc *http.ResponseController, c *connection) error {
	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	// no-store rather than no-cache: the App surface replaces a storable value under
	// a session (kit/httpx's caching rule), and a stream with no end has nothing to
	// revalidate.
	header.Set("Cache-Control", "no-store")
	header.Set("X-Accel-Buffering", "no")
	if err := h.write(w, rc, c, []byte(opening)); err != nil {
		return err
	}
	return h.write(w, rc, c, frame{event: "resync", data: resyncData{Time: h.stamp(), Reason: reasonConnect}}.bytes())
}

// write is one frame, one deadline and one flush.
//
// The deadline goes before every write, because a deadline set once at open bounds
// the connection's life and not the write, and a stream is meant to outlive a slow
// frame. The flush is where a stream either works or finds out it does not:
// http.ResponseController asks for FlushError before Flusher, so an error inside a
// wrapper that withholds cookies or applies a cache policy comes back here instead
// of being reported as success.
func (h *Hub) write(w http.ResponseWriter, rc *http.ResponseController, c *connection, body []byte) error {
	if err := h.bound(rc, w, c); err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("stream: the frame did not reach the connection: %w", err)
	}
	if err := rc.Flush(); err != nil {
		return fmt.Errorf("stream: the frame did not flush: %w", err)
	}
	return nil
}

// comment writes a line that reaches no handler in the browser. It is the last
// thing a connection whose session ended gets before it closes.
func (h *Hub) comment(w http.ResponseWriter, rc *http.ResponseController, c *connection, line string) error {
	if err := h.bound(rc, w, c); err != nil {
		return err
	}
	if _, err := w.Write([]byte(line)); err != nil {
		return err
	}
	return rc.Flush()
}

// bound sets the write deadline of one write. A writer with no deadlines is not a
// writer that failed — a buffered response, a test recorder — so the fact is said
// once per connection, the call is skipped from then on, and the connection is ended
// by a failed flush alone. Anything stricter would close every stream served through
// such a writer, including the ones this package's own cases run.
func (h *Hub) bound(rc *http.ResponseController, w http.ResponseWriter, c *connection) error {
	if c.unbounded {
		return nil
	}
	err := rc.SetWriteDeadline(h.clock.Now().Add(h.limits.WriteDeadline))
	if err == nil {
		return nil
	}
	if !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	c.unbounded = true
	h.log.InfoContext(context.Background(), "stream: this writer has no write deadlines; the connection is bounded by its flushes",
		"writer", fmt.Sprintf("%T", w))
	return nil
}

// loop is the connection: forward notices, re-ask the guards' questions at every
// heartbeat, and end when the client, the session or the writer ends. One goroutine
// writes to this response — this one, the request's own — so there is no interleaving
// to prevent, only a queue to bound.
func (h *Hub) loop(ctx context.Context, rc *http.ResponseController, w http.ResponseWriter, c *connection) error {
	ticker := time.NewTicker(h.limits.Heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.overflowed:
			// Exactly one resync per overflow episode: the flag is what a second
			// overflow finds already set, and a resync already on its way needs no
			// company. 200 notices against a blocked writer is one frame.
			if !c.overflow.CompareAndSwap(true, false) {
				continue
			}
			if err := h.write(w, rc, c, frame{event: "resync", data: resyncData{Time: h.stamp(), Reason: reasonOverflow}}.bytes()); err != nil {
				return err
			}
		case n := <-c.notices:
			if err := h.write(w, rc, c, frame{id: n.ID, event: n.Topic, data: noticeData{Time: stamp(n.At)}}.bytes()); err != nil {
				return err
			}
		case <-ticker.C:
			if err := h.beat(ctx, rc, w, c); err != nil {
				if errors.Is(err, errNoTopicsLeft) || errors.Is(err, errSessionEnded) {
					return nil
				}
				return err
			}
		}
	}
}

// beat is the heartbeat: the ping, and the two questions the request's guards asked
// once and a stream has to keep asking for as long as it outlives its own request. A
// grant withdrawn withdraws that topic and leaves the rest; an ended session ends the
// connection; the database's own failure is neither, and is asked again next tick.
//
// Both questions are asked of every connection, whatever its topics' policies. Which
// topics a caller may watch is not a fact that outlives the caller: a person who
// signed out, whose session was revoked or who left the tenant is nobody to this
// tenant's notices, whether the topic they watch names a grant, names a recipient or
// asks for nothing but a session. Asking only when a grant is named would make the
// cheapest topics — the ones every signed-in person may watch — the only ones that
// can outlive the session that admitted them.
func (h *Hub) beat(ctx context.Context, rc *http.ResponseController, w http.ResponseWriter, c *connection) error {
	h.mu.Lock()
	topics := make([]Topic, 0, len(c.topics))
	for _, t := range c.topics {
		topics = append(topics, t)
	}
	h.mu.Unlock()

	renewal, err := h.authority.Renew(ctx, c.tenant, c.request, grantsOf(topics))
	switch {
	case err != nil:
		// An outage is never an allowance, and never a reason to drop every
		// live connection in the tenant: nothing is delivered differently until
		// the next tick asks again.
		h.log.ErrorContext(ctx, "stream: the heartbeat could not re-check this connection",
			"tenant", c.tenant, "user", c.user, "error", err)
	case !renewal.SignedIn:
		_ = h.comment(w, rc, c, closing)
		return errSessionEnded
	case len(renewal.Refused) > 0:
		for _, t := range topics {
			if t.Policy.Kind() != module.KindGrant || !slices.Contains(renewal.Refused, t.Policy.Grant()) {
				continue
			}
			if err := h.withdraw(c, t, w, rc); err != nil {
				return err
			}
		}
	}
	return h.comment(w, rc, c, ping)
}
