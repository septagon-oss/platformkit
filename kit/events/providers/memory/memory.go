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

// observation is one watch. It holds no channel and no done: an observation is
// answered by nothing, which is what keeps a browser from being able to fail a
// publish or hold it open.
type observation struct {
	fn func(transport.Event)
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
	// event happened, and an event whose handling failed the publish did not. On the
	// publisher's goroutine, because nothing waits for an observer and an observer
	// that blocks must not be able to delay a publish either — the recover below is
	// the other half of the same promise: a panic in a watch is the watch's, and a
	// publish that failed because a browser's frame writer panicked is the failure
	// this prevents. The consequence is honest and stated in Observe: in-process
	// notices are as prompt as this process's handlers.
	for _, o := range watching {
		m.notify(ctx, o, ev)
	}
	return nil
}

func (m *memory) notify(ctx context.Context, o *observation, ev transport.Event) {
	defer func() {
		if v := recover(); v != nil {
			slog.ErrorContext(ctx, "events: an observation panicked; the notice was dropped",
				"event", ev.Name, "id", ev.ID, "tenant", ev.TenantID, "panic", v)
		}
	}()
	o.fn(ev)
}

// Observe watches one tenant's one event inside this process. Routing is by the
// pair, which is the address the broker provider filters: an event of another
// tenant is a different key, so no filter here is wide and nothing has to be
// filtered out of it. The observation shares this transport's fate with the
// publishers of one process, which is what a notice means here: the durable
// claims, the retries and the replay belong to Subscribe.
func (m *memory) Observe(_ context.Context, tenantID uuid.UUID, name string, fn func(transport.Event)) (func(), error) {
	key := observerKey{tenant: tenantID, name: name}
	o := &observation{fn: fn}
	m.mu.Lock()
	m.observers[key] = append(m.observers[key], o)
	m.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
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
		})
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
