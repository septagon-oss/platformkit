package events

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"
)

const queue = 256

// Memory delivers inside one process. Publish waits for committed handling or
// terminal recording before the relay may stamp its outbox row. The worker's
// context is separate from the relay transaction and survives a short relay
// deadline. A process restart recovers unfinished work from the unstamped outbox.
func Memory() Transport { return &memory{subs: map[string][]localSubscription{}} }

type memory struct {
	mu   sync.Mutex
	subs map[string][]localSubscription
}

type localSubscription struct {
	ctx    context.Context
	events chan delivery
}

type delivery struct {
	event Event
	done  chan error
}

func (m *memory) Publish(ctx context.Context, ev Event) error {
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
func (m *memory) Subscribe(ctx context.Context, _, name string, sink Sink) error {
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

func (m *memory) deliver(ctx context.Context, ev Event, sink Sink) error {
	var cause error
	for attempt := 1; ; attempt++ {
		if attempt <= maxDeliveries {
			cause = sink.Handle(ctx, ev)
			if cause == nil {
				return nil
			}
		}
		if attempt >= maxDeliveries {
			slog.ErrorContext(ctx, "events: recording terminal failure", "event", ev.Name, "id", ev.ID, "error", cause)
			if err := sink.Dead(ctx, ev, cause); err == nil {
				return nil
			} else {
				slog.ErrorContext(ctx, "events: terminal recording failed, retrying", "event", ev.Name, "id", ev.ID, "error", err)
			}
		}
		wait := backoff[min(attempt, len(backoff))-1]
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
