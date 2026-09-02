package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

func (j *jetstream) Subscribe(ctx context.Context, durable, name string, h func(context.Context, Event) error) error {
	_, err := j.js.Subscribe(subject+name, func(msg *nats.Msg) {
		var ev Event
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			// A message that will never parse would be redelivered forever.
			// Terminate it and say so; the outbox still holds the row.
			slog.ErrorContext(ctx, "events: undecodable message", "subject", msg.Subject, "error", err)
			_ = msg.Term()
			return
		}
		if err := h(ctx, ev); err != nil {
			slog.WarnContext(ctx, "events: handler failed, redelivering",
				"event", ev.Name, "id", ev.ID, "error", err)
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	}, nats.Durable(durable), nats.ManualAck(), nats.AckExplicit(), nats.DeliverAll(),
		nats.AckWait(30*time.Second), nats.BindStream(stream))
	if err != nil {
		return fmt.Errorf("events: subscribe %s to %s: %w", durable, name, err)
	}
	return nil
}

// Close releases the connection. It is not part of Transport, because the
// memory transport has nothing to release; kit/app asks for io.Closer.
func (j *jetstream) Close() error {
	j.nc.Close()
	return nil
}
