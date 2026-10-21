// Package memory delivers committed events inside one process.
// Durable recovery belongs to the caller; the PlatformKit outbox retains an
// unstamped row until Publish reports that its subscriptions finished.
package memory

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	deliverypolicy "github.com/septagon-oss/platformkit/kit/events/internal/delivery"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

const queue = 256

// New delivers inside one process. Publish waits for every subscribed sink to
// report successful handling or terminal recording; cancellation can leave that
// delivery running under the subscription's context. With events.Consume and
// the SQL outbox, success follows a committed claim and an unfinished publication
// leaves its outbox row pending for replay after restart. Standalone sinks must
// provide their own persistence and replay source.
//
// The app the envelope carries passes through untouched. It is the transport that
// routes by an address that both stamps its own app into the document and has its
// delivery checked against it (transport.AddressMismatch, asked by the delivery);
// this transport routes by event name inside one process, so it has no address to
// disagree with, and it neither claims an app nor refuses one. What the two
// transports share is the decision the field exists for: a delivery runs where its
// claims can be checked, and the claim ledger is the same table either way.
func New() transport.Transport {
	return &memory{subs: map[string][]localSubscription{}, observers: map[observerKey][]*observation{}}
}

type memory struct {
	mu   sync.Mutex
	subs map[string][]localSubscription
	// observers are the ephemeral watches, keyed by the pair a durable
	// subscription is not keyed by. A subscription answers an event name and takes
	// the tenant with whatever the message claims; an observation is addressed, so
	// tenant and name together are its filter and no delivery of another tenant's
	// event can reach it.
	observers map[observerKey][]*observation
}

// observerKey is one address observed: this tenant's name, and nothing else.
type observerKey struct {
	tenant uuid.UUID
	name   string
}

// observation is one watch: the notice goes to a queue this transport owns and the
// watch's own goroutine reads it, so a callback that does not return can neither
// hold a publication open nor fail one.
//
// The queue is bounded by the same number a subscription's is, and a notice that
// finds it full is dropped: the contract this transport answers to is "a delivery
// that cannot be handed over is lost", and the alternative — an unbounded queue, or
// a publish that waits — is a browser's stalled reader becoming the outbox relay's
// stalled reader. A watch that loses notices is a watch whose caller is told it is
// not current, which is what transport.Observe's resync is for.
type observation struct {
	queue chan transport.Event
	// closing is the cancellation of the context the observation was opened on.
	// It is what ends the delivering goroutine when the caller withdraws by context
	// rather than by stop, and closing it twice is the panic this design cannot have.
	closing <-chan struct{}
	once    sync.Once
	stop    chan struct{}
}

type localSubscription struct {
	ctx    context.Context
	events chan delivery
}

type delivery struct {
	event transport.Event
	done  chan error
}

func (m *memory) Publish(ctx context.Context, ev transport.Event) error {
	m.mu.Lock()
	subs := slices.Clone(m.subs[ev.Name])
	watching := slices.Clone(m.observers[observerKey{tenant: ev.TenantID, name: ev.Name}])
	m.mu.Unlock()
	requests := make([]delivery, len(subs))
	for i, sub := range subs {
		request := delivery{event: ev, done: make(chan error, 1)}
		requests[i] = request
		select {
		case sub.events <- request:
		case <-sub.ctx.Done():
			return sub.ctx.Err()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for i, sub := range subs {
		select {
		case err := <-requests[i].done:
			if err != nil {
				return err
			}
		case <-sub.ctx.Done():
			return sub.ctx.Err()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	// Last, after every handler reported success: a notice is the fact that this
	// event happened, and an event whose handling failed the publish did not. Handed
	// to each watch's own queue rather than called here, because the durable work
	// above is what Publish is for and a watch is what it is beside: an observation
	// that stops returning is a browser that stopped reading, and the transport's
	// promise — fn runs on a goroutine the transport owns, and a transport never
	// waits for it — is what keeps that browser from holding the outbox relay open.
	// The consequence is honest and stated in Observe: in-process notices are as
	// prompt as this process's delivery goroutines are.
	for _, o := range watching {
		o.offer(ctx, ev)
	}
	return nil
}

// offer hands one notice to one watch and does not wait for it: the send that finds
// the queue full drops this notice, and the watch keeps its place in line for the
// next one.
func (o *observation) offer(ctx context.Context, ev transport.Event) {
	select {
	case o.queue <- ev:
	default:
		// One line per dropped notice, which is one line per event a watch was too
		// slow for: the alternative is a stream that says nothing while it goes stale.
		slog.WarnContext(ctx, "events: an observation was behind; the notice was dropped",
			"event", ev.Name, "id", ev.ID, "tenant", ev.TenantID, "queue", cap(o.queue))
	}
}

// run is the goroutine an observation owns. It delivers alone, so one publish reaches
// one watch once, and it ends when the watch is withdrawn by either door its caller
// has: the context it was opened on, or its stop function. A panic inside the
// callback is the callback's and ends this watch — the publish that caused it already
// returned, and a watch cannot be asked to recover on the transport's behalf.
func (o *observation) run(ctx context.Context, fn func(transport.Event)) {
	for {
		var ev transport.Event
		select {
		case ev = <-o.queue:
		case <-o.closing:
			return
		case <-o.stop:
			return
		}
		if handedOver(ctx, ev, fn) {
			return
		}
	}
}

// handedOver delivers one notice and reports whether the watch should go on: false
// unless the callback panicked, which is the one failure a watch is not asked to get
// over.
func handedOver(ctx context.Context, ev transport.Event, fn func(transport.Event)) (stop bool) {
	defer func() {
		if v := recover(); v != nil {
			slog.ErrorContext(ctx, "events: an observation panicked; no further notice reaches this watch",
				"event", ev.Name, "id", ev.ID, "tenant", ev.TenantID, "panic", v)
			stop = true
		}
	}()
	fn(ev)
	return false
}

// Observe watches one tenant's one event inside this process. Routing is by the
// pair, which is the address the broker provider filters: an event of another
// tenant is a different key, so no filter here is wide and nothing has to be
// filtered out of it. The observation shares this transport's fate with the
// publishers of one process, which is what a notice means here: the durable
// claims, the retries and the replay belong to Subscribe.
//
// fn runs on a goroutine this transport opens here and on no other, and it is never
// waited for: Publish returns once the notice is queued. A watch that reads slower
// than this process publishes loses notices at its own queue's length, and a watch
// that stops losing them is a watch whose caller has to be told it is not current —
// transport.Observe promises a dropped notice is not owed.
func (m *memory) Observe(ctx context.Context, tenantID uuid.UUID, name string, fn func(transport.Event)) (func(), error) {
	key := observerKey{tenant: tenantID, name: name}
	o := &observation{queue: make(chan transport.Event, queue), closing: ctx.Done(), stop: make(chan struct{})}
	m.mu.Lock()
	m.observers[key] = append(m.observers[key], o)
	m.mu.Unlock()
	go o.run(ctx, fn)
	return func() {
		o.once.Do(func() { close(o.stop) })
		m.mu.Lock()
		defer m.mu.Unlock()
		var kept []*observation
		for _, existing := range m.observers[key] {
			if existing != o {
				kept = append(kept, existing)
			}
		}
		if len(kept) == 0 {
			delete(m.observers, key)
			return
		}
		m.observers[key] = kept
	}, nil
}

// Each subscription serializes its own work. A publisher that times out leaves
// its buffered completion unread; the next relay retries and the database claim
// skips work already committed, including a committed terminal outcome.
func (m *memory) Subscribe(ctx context.Context, _, name string, sink transport.Sink) error {
	sub := localSubscription{ctx: ctx, events: make(chan delivery, queue)}
	m.mu.Lock()
	m.subs[name] = append(m.subs[name], sub)
	m.mu.Unlock()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case request := <-sub.events:
				request.done <- m.deliver(ctx, request.event, sink)
			}
		}
	}()
	return nil
}

func (m *memory) deliver(ctx context.Context, ev transport.Event, sink transport.Sink) error {
	var cause error
	for attempt := 1; ; attempt++ {
		if attempt <= deliverypolicy.MaxDeliveries {
			cause = sink.Handle(ctx, ev)
			if cause == nil {
				return nil
			}
		}
		if attempt >= deliverypolicy.MaxDeliveries {
			slog.ErrorContext(ctx, "events: recording terminal failure", "event", ev.Name, "id", ev.ID, "error", cause)
			if err := sink.Dead(ctx, ev, cause); err == nil {
				return nil
			} else {
				slog.ErrorContext(ctx, "events: terminal recording failed, retrying", "event", ev.Name, "id", ev.ID, "error", err)
			}
		}
		wait := deliverypolicy.Backoff[min(attempt, len(deliverypolicy.Backoff))-1]
		slog.WarnContext(ctx, "events: delivery unfinished, retrying",
			"event", ev.Name, "id", ev.ID, "attempt", attempt, "in", wait, "error", cause)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
