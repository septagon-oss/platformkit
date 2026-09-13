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
func New() transport.Transport { return &memory{subs: map[string][]localSubscription{}} }

type memory struct {
	mu   sync.Mutex
	subs map[string][]localSubscription
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
	return nil
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
