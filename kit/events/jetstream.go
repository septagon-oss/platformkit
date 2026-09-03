package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/nats-io/nats.go"
)

// The one stream and its one subject space. Every PlatformKit event is
// published as platformkit.<name>, so a consumer filters by subject and an
// operator sees the whole traffic under one prefix.
const (
	stream  = "PLATFORMKIT"
	subject = "platformkit."
)

// JetStream is the transport for a fleet: NATS JetStream, one stream, durable
// consumers, explicit acknowledgement. A handler that returns an error nacks,
// and JetStream redelivers; a handler that succeeds acks, and the event is that
// consumer's history.
//
// The returned Transport is an io.Closer, so kit/app releases the connection
// when the worker stops.
func JetStream(url string) (Transport, error) {
	nc, err := nats.Connect(url, nats.Name("platformkit"), nats.MaxReconnects(-1))
	if err != nil {
		return nil, fmt.Errorf("events: connect to %s: %w", url, err)
	}
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("events: jetstream: %w", err)
	}
	// Created on first use rather than by a deployment step, so a fresh
	// environment needs a NATS and nothing else. Retention is by age: the
	// outbox is the durable copy, the stream is the delivery path.
	if _, err := js.StreamInfo(stream); errors.Is(err, nats.ErrStreamNotFound) {
		_, err = js.AddStream(&nats.StreamConfig{
			Name:      stream,
			Subjects:  []string{subject + ">"},
			Retention: nats.LimitsPolicy,
			Storage:   nats.FileStorage,
			MaxAge:    keep,
		})
		if err != nil {
			nc.Close()
			return nil, fmt.Errorf("events: create the %s stream: %w", stream, err)
		}
	} else if err != nil {
		nc.Close()
		return nil, fmt.Errorf("events: read the %s stream: %w", stream, err)
	}
	return &jetstream{nc: nc, js: js}, nil
}

type jetstream struct {
	nc *nats.Conn
	js nats.JetStreamContext
}

func (j *jetstream) Publish(ctx context.Context, ev Event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("events: marshal %s: %w", ev.Name, err)
	}
	if _, err := j.js.Publish(subject+ev.Name, body, nats.Context(ctx)); err != nil {
		return fmt.Errorf("events: publish %s: %w", ev.Name, err)
	}
	return nil
}

// ackWait is how long JetStream waits for an acknowledgement before it decides
// a delivery was lost, and it is the first rung of the redelivery ladder rather
// than a number beside it.
//
// That is not tidiness, it is the drift that had every worker crashlooping. The
// server pins a consumer's AckWait to BackOff[0] whenever a backoff is set, so
// a subscription that asked for thirty seconds under a ladder starting at one
// second was stored with one — and nats.go refuses a later subscription whose
// stored configuration is not the one it asks for:
//
//	nats: configuration requests ack wait to be 30s, but consumer's value is 1s
//
// The first process therefore created a consumer no later process could bind
// to. The web role stayed up, every worker crashlooped, and nothing in the
// chart could see it. One value used twice cannot drift from itself; reconcile
// handles the drift that is still possible, which is a deploy that changes the
// ladder itself.
func ackWait() time.Duration { return backoff[0] }

// wanted is the consumer this code asks for. It is one value because the
// subscription below and reconcile have to ask for the same thing: two lists of
// the same settings is how a consumer comes to differ from the code that
// created it.
func wanted(durable, name string) []nats.SubOpt {
	return []nats.SubOpt{
		nats.Durable(durable), nats.ManualAck(), nats.AckExplicit(), nats.DeliverAll(),
		nats.AckWait(ackWait()), nats.MaxDeliver(maxDeliveries), nats.BackOff(backoff),
		nats.BindStream(stream),
	}
}

// reconcile brings a stored consumer into line with what this code asks for,
// before the subscription that would otherwise be refused for the difference.
//
// A durable consumer outlives the process that created it, so its settings are
// a second copy of three constants — and a deploy that changes one of them used
// to mean a worker that could not boot until somebody deleted the consumer by
// hand. NATS can change some of them on a live consumer and not others, so this
// updates what it can and deletes what it cannot, naming the field either way.
//
// Deleting is safe and worth saying why: this code asks for DeliverAll, so a
// recreated consumer replays the stream from the beginning, and every replay is
// claimed in platformkit_handled before the handler runs. A handler that has
// already run does not run again; one that never ran gets its event. That is
// the same guarantee an ordinary redelivery has (docs/adr/0004), which is why
// recreating a consumer is a log line rather than an operator's afternoon.
func (j *jetstream) reconcile(ctx context.Context, durable, name string) error {
	info, err := j.js.ConsumerInfo(stream, durable, nats.Context(ctx))
	switch {
	case errors.Is(err, nats.ErrConsumerNotFound):
		return nil // The subscription below creates it, with the settings above.
	case err != nil:
		return fmt.Errorf("read the consumer: %w", err)
	}

	want := info.Config
	var changed, immutable []string
	if info.Config.AckWait != ackWait() {
		changed = append(changed, fmt.Sprintf("ack_wait %s to %s", info.Config.AckWait, ackWait()))
		want.AckWait = ackWait()
	}
	if info.Config.MaxDeliver != maxDeliveries {
		changed = append(changed, fmt.Sprintf("max_deliver %d to %d", info.Config.MaxDeliver, maxDeliveries))
		want.MaxDeliver = maxDeliveries
	}
	if !slices.Equal(info.Config.BackOff, backoff) {
		changed = append(changed, fmt.Sprintf("backoff %v to %v", info.Config.BackOff, backoff))
		want.BackOff = backoff
	}
	// The ones an update cannot carry: what a consumer filters, how it
	// acknowledges, where it starts, and whether it is pushed at all. A
	// consumer that differs in any of them is not this subscription's consumer
	// wearing the wrong settings, it is somebody else's under the same name.
	if info.Config.FilterSubject != subject+name {
		immutable = append(immutable, fmt.Sprintf("filter_subject %q to %q", info.Config.FilterSubject, subject+name))
	}
	if info.Config.AckPolicy != nats.AckExplicitPolicy {
		immutable = append(immutable, fmt.Sprintf("ack_policy %s to explicit", info.Config.AckPolicy))
	}
	if info.Config.DeliverPolicy != nats.DeliverAllPolicy {
		immutable = append(immutable, fmt.Sprintf("deliver_policy %d to all", info.Config.DeliverPolicy))
	}
	if info.Config.DeliverSubject == "" {
		immutable = append(immutable, "pull to push")
	}

	switch {
	case len(immutable) > 0:
		slog.WarnContext(ctx, "events: the stored consumer differs in a setting NATS cannot change; deleting it so this subscription creates it again",
			"durable", durable, "fields", immutable, "also", changed)
		if err := j.js.DeleteConsumer(stream, durable, nats.Context(ctx)); err != nil {
			return fmt.Errorf("delete the drifted consumer: %w", err)
		}
	case len(changed) > 0:
		slog.InfoContext(ctx, "events: reconciling the stored consumer with this build",
			"durable", durable, "fields", changed)
		if _, err := j.js.UpdateConsumer(stream, &want, nats.Context(ctx)); err != nil {
			return fmt.Errorf("update the drifted consumer: %w", err)
		}
	}
	return nil
}

func (j *jetstream) Subscribe(ctx context.Context, durable, name string, sink Sink) error {
	if err := j.reconcile(ctx, durable, name); err != nil {
		return fmt.Errorf("events: subscribe %s to %s: %w", durable, name, err)
	}
	sub, err := j.js.Subscribe(subject+name, func(msg *nats.Msg) {
		var ev Event
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			// A message that will never parse would be redelivered forever.
			// Terminate it and say so; the outbox still holds the row.
			slog.ErrorContext(ctx, "events: undecodable message", "subject", msg.Subject, "error", err)
			_ = msg.Term()
			return
		}
		err := sink.Handle(ctx, ev)
		if err == nil {
			_ = msg.Ack()
			return
		}
		// Nak alone, with no delivery cap, is what turned one poison event into
		// a redelivery storm: the server has nothing to wait for and hands it
		// straight back. MaxDeliver and BackOff below bound that, and this is
		// the end of the ladder — terminate the message so it stops coming, and
		// record it, because an event that is simply dropped is an integration
		// that failed silently.
		if last := exhausted(msg); last {
			_ = msg.Term()
			sink.Dead(ctx, ev, err)
			return
		}
		slog.WarnContext(ctx, "events: handler failed, redelivering",
			"event", ev.Name, "id", ev.ID, "error", err)
		_ = msg.Nak()
	}, wanted(durable, name)...)
	if err != nil {
		return fmt.Errorf("events: subscribe %s to %s: %w", durable, name, err)
	}
	// The subscription outlives this call, so the worker's shutdown has to
	// reach it: without this a handler kept running after the context was
	// cancelled, on a connection nobody was closing yet.
	go func() {
		<-ctx.Done()
		_ = sub.Drain()
	}()
	return nil
}

// exhausted reports whether this was the last delivery JetStream will make.
// Metadata is unavailable on a message that did not come from a stream, and the
// safe reading of "I cannot tell" is that there are more attempts to come —
// terminating on a doubt would drop an event that was going to succeed.
func exhausted(msg *nats.Msg) bool {
	meta, err := msg.Metadata()
	return err == nil && meta.NumDelivered >= uint64(maxDeliveries)
}

// Close releases the connection. It is not part of Transport, because the
// memory transport has nothing to release; kit/app asks for io.Closer.
func (j *jetstream) Close() error {
	j.nc.Close()
	return nil
}
