package events

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// queue is how many events one subscription may fall behind before a publisher
// waits for it. In a single process the relay is the only publisher, so waiting
// simply slows the next relay pass, which is the correct back pressure.
const queue = 256

// Memory is the in-process transport: a single-process deployment and every
// test that does not want a broker. Delivery is asynchronous — the relay hands
// the event over and returns, and the handler runs on the subscription's own
// goroutine — because a handler opens a tenant transaction and the relay's
// transaction is a system one, which it could not nest in.
func Memory() Transport { return &memory{subs: map[string][]chan Event{}} }

type memory struct {
	mu   sync.Mutex
	subs map[string][]chan Event
}

// Publish hands the event to every subscription of that name. An event nobody
// subscribes to is delivered nowhere and that is not an error: the relay's job
// is to get it out of the database, not to find it a reader.
//
// A subscription that has fallen a full queue behind makes this block, and the
// select on ctx.Done is what keeps that from being a deadlock: the relay runs
// under a deadline, so a wedged subscription costs one abandoned relay pass and
// the rows go again next tick. Before the deadline existed, one poison handler
// stopped the relay, and with it every other subscription and every other job.
func (m *memory) Publish(ctx context.Context, ev Event) error {
	m.mu.Lock()
	chans := slices.Clone(m.subs[ev.Name])
	m.mu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- ev:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Subscribe starts one goroutine per subscription. durable is unused: there is
// nothing to resume across a restart when the queue lives in the process that
// restarted, and an event that was in flight is still unstamped in the outbox,
// so the relay sends it again.
func (m *memory) Subscribe(ctx context.Context, _, name string, sink Sink) error {
	ch := make(chan Event, queue)
	m.mu.Lock()
	m.subs[name] = append(m.subs[name], ch)
	m.mu.Unlock()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-ch:
				m.deliver(ctx, ev, sink)
			}
		}
	}()
	return nil
}

// deliver retries one event on the backoff ladder and gives up after
// maxDeliveries, which is the same policy the JetStream transport applies with
// nats.MaxDeliver: two transports that disagreed about when to stop would be
// two policies to reason about during an incident.
//
// It holds the subscription while it retries, so a poison event delays that
// subscription rather than being dropped silently — and then dead-letters,
// which is what stops it delaying anything forever.
func (m *memory) deliver(ctx context.Context, ev Event, sink Sink) {
	for attempt := 1; ; attempt++ {
		err := sink.Handle(ctx, ev)
		if err == nil {
			return
		}
		if attempt >= maxDeliveries {
			sink.Dead(ctx, ev, err)
			return
		}
		wait := backoff[min(attempt, len(backoff))-1]
		slog.WarnContext(ctx, "events: handler failed, redelivering",
			"event", ev.Name, "id", ev.ID, "attempt", attempt, "in", wait, "error", err)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
