package stream

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/module"
)

// deliver hands one event to every connection watching that address whose own
// policy lets it see this one. It never blocks and never fails: a connection whose
// queue is full is a connection that will be told it missed something, and a
// publisher is never delayed by a subscriber that stopped reading.
func (h *Hub) deliver(key watchKey, ev transport.Event) {
	// The connections and each one's own view of the topic are read together under
	// the mutex, because a withdrawal and a delivery are the same decision made from
	// two directions. The notices then go out outside it.
	type address struct {
		c     *connection
		topic Topic
	}
	h.mu.Lock()
	w, ok := h.watches[key]
	if !ok {
		h.mu.Unlock()
		return
	}
	targets := make([]address, 0, len(w.conns))
	for c := range w.conns {
		t, watching := c.topics[ev.Name]
		if !watching {
			// A connection that withdrew this topic while the watch was shared by
			// another of its own connections: its queue is no longer this one's
			// business.
			continue
		}
		targets = append(targets, address{c: c, topic: t})
	}
	h.mu.Unlock()

	notice := Notice{ID: ev.ID, Topic: ev.Name, At: ev.At}
	for _, target := range targets {
		if !h.maySee(target.c, target.topic, ev) {
			continue
		}
		select {
		case target.c.notices <- notice:
		default:
			// Overflow, once: the queue is dropped, so what reaches the connection
			// after this is the resync and then the live edge, and one frame says
			// both.
			if target.c.overflow.CompareAndSwap(false, true) {
				select {
				case target.c.overflowed <- struct{}{}:
				default:
				}
			}
			// Each receive is guarded by a default rather than by a length check,
			// because this runs on the goroutine of whoever published the event: between
			// "there is something in there" and "I took it" the connection's own writer
			// may have taken the last one, and a publisher waiting on a queue that has
			// just gone empty is a slow reader stalling the writer of the event.
		drain:
			for {
				select {
				case <-target.c.notices:
				default:
					break drain
				}
			}
		}
	}
}

// watching reports whether this connection still holds one topic. It is the question
// a notice has to pass on its way to the wire, and it is asked under the same mutex as
// the withdrawal that would take the topic away, so a frame for a grant the caller has
// lost cannot slip past the decision that withdrew it.
func (h *Hub) watching(c *connection, event string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := c.topics[event]
	return ok
}

// maySee applies a topic's per-notice filter. Only StreamRecipient has one: the
// server reads the named member of the payload, compares it to the connection's own
// principal, and lets no part of the payload go further than this function. A member
// that is not a user id is nobody's notice — dropped, and said once per event,
// because a payload that cannot answer who it is for reaches nobody.
func (h *Hub) maySee(c *connection, t Topic, ev transport.Event) bool {
	if t.Policy.Kind() != module.KindRecipient {
		return true
	}
	id, ok := namedUser(ev.Payload, t.Policy.Field())
	if !ok {
		h.log.ErrorContext(context.Background(), "stream: a recipient topic names no readable recipient",
			"event", ev.Name, "id", ev.ID, "field", t.Policy.Field())
		return false
	}
	return id == c.user
}

// namedUser reads one member of a payload as a user id. The payload is parsed as
// members rather than against a schema, because the schema of an event lives in
// kit/events' declared payload type and a second one here would be a second one to
// keep honest.
func namedUser(payload []byte, field string) (uuid.UUID, bool) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(payload, &members); err != nil {
		return uuid.Nil, false
	}
	var id uuid.UUID
	if err := json.Unmarshal(members[field], &id); err != nil {
		return uuid.Nil, false
	}
	return id, id != uuid.Nil
}

// withdraw drops one topic of a live connection: its frame goes out, its watch is
// released, and the connection itself ends when nothing is left to watch. What it does
// not do is drain the connection's queue — the notices already there belong to a
// writer that has not decided what to send, and loop drops them at the write.
func (h *Hub) withdraw(c *connection, t Topic, w http.ResponseWriter, rc *http.ResponseController) error {
	h.mu.Lock()
	delete(c.topics, t.Event)
	stop, _ := h.dropWatch(watchKey{tenant: c.tenant.ID, event: t.Event}, c)
	empty := len(c.topics) == 0
	h.mu.Unlock()
	if stop != nil {
		defer stop()
	}
	if err := h.write(w, rc, c, frame{event: "dropped", data: droppedData{Topic: t.Event, Reason: reasonPermission}}.bytes()); err != nil {
		return err
	}
	if empty {
		return errNoTopicsLeft
	}
	return nil
}
