package stream

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type watchKey struct {
	tenant uuid.UUID
	event  string
}

// seatKey is one person's slots in one tenant. The ceiling counts this person's
// tabs, and a tab is opened inside a tenant: two tenants of one account are two
// browsers' worth of connections, not one that has spent its share.
type seatKey struct {
	tenant uuid.UUID
	user   uuid.UUID
}

// watch is one (tenant, event) observation shared by every connection in the hub
// that asked for it. conns is read under the hub's mutex and never while a notice
// is being handed to anyone.
//
// stop ends the observation and the context it was opened on, so it belongs to the
// watch and not to any one of the connections sharing it: the last one to leave is
// the one that stops it, and the one that happened to open it is only whoever got
// there first.
type watch struct {
	stop  func()
	conns map[*connection]struct{}
}

type connection struct {
	user   uuid.UUID
	tenant tenancy.Tenant
	// request is the one this connection arrived on: the heartbeat re-checks the
	// session of that caller, which is the only caller a stream may speak for.
	request *http.Request
	// topics is the connection's own view of what it still watches, and is guarded
	// by the hub's mutex — the same one the watches are guarded by, because a
	// delivery and a withdrawal have to agree.
	topics map[string]Topic
	// notices is the per-connection queue. It is never closed: a connection that
	// left simply stops reading, which is the whole of what a stop racing a delivery
	// needs, and a send to a closed channel is not an option anyone has to remember.
	notices chan Notice
	// overflowed wakes the writer once per overflow episode; overflow is the flag a
	// second overflow finds already set.
	overflowed chan struct{}
	overflow   atomic.Bool
	// unbounded says this writer has no write deadlines, which is a fact about the
	// writer rather than a failure of it, and is learned once.
	unbounded bool
}

// register takes one connection's slots: this person's share of them in this
// tenant, and this tenant's share of the process. The count and the check are one
// critical section, so two connections racing for the last slot cannot both take it.
func (h *Hub) register(req Request, topics []Topic) (*connection, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	seat := seatKey{tenant: req.Tenant.ID, user: req.Principal.UserID}
	if h.perPrincipal[seat] >= h.limits.MaxPerPrincipal {
		return nil, &Refusal{Status: http.StatusTooManyRequests, RetryAfter: h.limits.RetryAfter,
			Detail: httpx.CodeLimitExhausted + fmt.Sprintf(": this caller already holds %d live connections in this tenant; close one before opening another", h.limits.MaxPerPrincipal)}
	}
	if h.perTenant[req.Tenant.ID] >= h.limits.MaxPerTenant {
		return nil, &Refusal{Status: http.StatusTooManyRequests, RetryAfter: h.limits.RetryAfter,
			Detail: httpx.CodeLimitExhausted + fmt.Sprintf(": this tenant already holds %d live connections in this process", h.limits.MaxPerTenant)}
	}
	h.perPrincipal[seat]++
	h.perTenant[req.Tenant.ID]++
	c := &connection{
		user:       req.Principal.UserID,
		tenant:     req.Tenant,
		request:    req.HTTP,
		topics:     make(map[string]Topic, len(topics)),
		notices:    make(chan Notice, h.limits.Queue),
		overflowed: make(chan struct{}, 1),
	}
	for _, t := range topics {
		c.topics[t.Event] = t
	}
	return c, nil
}

// watchTopic joins or opens one observation per tenant and event. A reservation
// serializes dialing without holding the mutex across Observe. The watch owns
// its context, survives the first connection, and closes with its last holder.
func (h *Hub) watchTopic(ctx context.Context, c *connection, t Topic) error {
	key := watchKey{tenant: c.tenant.ID, event: t.Event}
	dial, err := h.reserveWatch(ctx, key, c)
	if err != nil || !dial {
		return err
	}
	return h.dialWatch(ctx, key, c)
}

// reserveWatch answers one connection's ask for one watch key: it takes the
// connection's place in a live watch and reports false, or reports true to the one
// connection that is to open it. A failed dial leaves no watch and no slot behind, so
// a connection that waited takes its own turn rather than inheriting a failure that
// was somebody else's round trip.
func (h *Hub) reserveWatch(ctx context.Context, key watchKey, c *connection) (dial bool, err error) {
	h.mu.Lock()
	w, live := h.watches[key]
	opening, dialling := h.dialling[key]
	if live {
		w.conns[c] = struct{}{}
		h.mu.Unlock()
		return false, nil
	}
	if dialling {
		// Waiting is the whole cost of opening an address once. Whoever dialled owns
		// the round trip; this connection asks again the moment it ends, and joins the
		// watch it installed — or takes its turn if there is nothing to join.
		h.mu.Unlock()
		select {
		case <-opening:
			return h.reserveWatch(ctx, key, c)
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	h.dialling[key] = make(chan struct{})
	h.mu.Unlock()
	return true, nil
}

// dialWatch opens one address through the transport and installs the watch that every
// connection waiting on it will join. The slot taken by reserveWatch is what makes
// this the only writer for the key, so the map holds one observation per address no
// matter how many connections arrived at it at once.
func (h *Hub) dialWatch(ctx context.Context, key watchKey, c *connection) error {
	life, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop, err := h.transport.Observe(life, key.tenant, key.event, func(ev transport.Event) {
		h.deliver(key, ev)
	})
	leave := func() {
		cancel()
		stop()
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if opening, ok := h.dialling[key]; ok {
		delete(h.dialling, key)
		close(opening)
	}
	if err != nil {
		// The context this watch would have lived on is the only thing it held, and it
		// goes with the failure: a transport that refused the consumer keeps no
		// observation, and nothing of this one is left running.
		cancel()
		return err
	}
	h.watches[key] = &watch{stop: leave, conns: map[*connection]struct{}{c: {}}}
	return nil
}

// release gives back the connection's slots and every watch it held. The last holder
// of a watch stops it, outside the mutex: a withdrawal never waits on a delivery, and
// no broker round trip belongs in a critical section.
func (h *Hub) release(c *connection) {
	h.mu.Lock()
	seat := seatKey{tenant: c.tenant.ID, user: c.user}
	if h.perPrincipal[seat]--; h.perPrincipal[seat] <= 0 {
		delete(h.perPrincipal, seat)
	}
	if h.perTenant[c.tenant.ID]--; h.perTenant[c.tenant.ID] <= 0 {
		delete(h.perTenant, c.tenant.ID)
	}
	var stopped []func()
	for event := range c.topics {
		if stop, gone := h.dropWatch(watchKey{tenant: c.tenant.ID, event: event}, c); gone {
			stopped = append(stopped, stop)
		}
	}
	h.mu.Unlock()
	for _, stop := range stopped {
		stop()
	}
}

// dropWatch removes one connection from one watch and reports the stop function when
// it was the last holder. It runs under the hub's mutex.
func (h *Hub) dropWatch(key watchKey, c *connection) (func(), bool) {
	w, ok := h.watches[key]
	if !ok {
		return nil, false
	}
	delete(w.conns, c)
	if len(w.conns) > 0 {
		return nil, false
	}
	delete(h.watches, key)
	return w.stop, true
}
