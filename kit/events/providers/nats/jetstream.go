// Package nats provides the existing JetStream event transport.
// The caller owns tenant checks and idempotent or transactional sink handling;
// what this provider owns is the address it routed by, and a delivery whose
// document does not claim that address is terminated before the sink runs.
package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/septagon-oss/platformkit/kit/appname"
	delivery "github.com/septagon-oss/platformkit/kit/events/internal/delivery"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

// The one stream. Its subject space is transport.Subject:
// platformkit.<tenant>.<module>.<event>, so a consumer filters by subject, an
// operator sees the whole traffic under one prefix, and a tenant's own backlog
// is an address rather than a query. See kit/events/transport/subject.go.
const stream = "PLATFORMKIT"

// JetStream is the transport for a fleet: NATS JetStream, one stream, durable
// consumers, explicit acknowledgement. Failed deliveries remain unacknowledged
// for the consumer backoff to redeliver; a successful handler acknowledges.
//
// The returned transport.Transport is an io.Closer, so kit/app releases the connection
// when the worker stops. Native NATS options let the composition supply TLS
// trust and credentials without putting secrets in the endpoint URL. Options
// follow the transport defaults, so the composition can override them.
func JetStream(app appname.Name, url string, options ...nats.Option) (transport.Transport, error) {
	options = append([]nats.Option{nats.Name(appname.ConnectionName(appname.Prefix, app)), nats.MaxReconnects(-1)}, options...)
	nc, err := nats.Connect(url, options...)
	if err != nil {
		return nil, connectionError{cause: err}
	}
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("events: jetstream: %w", err)
	}
	// Created on first use rather than by a deployment step, so a fresh
	// environment needs a NATS and nothing else.
	if _, err := js.StreamInfo(stream); errors.Is(err, nats.ErrStreamNotFound) {
		if _, err := js.AddStream(wantedStream()); err != nil {
			nc.Close()
			return nil, fmt.Errorf("events: create the %s stream: %w", stream, err)
		}
	} else if err != nil {
		nc.Close()
		return nil, fmt.Errorf("events: read the %s stream: %w", stream, err)
	}
	return &jetstream{nc: nc, js: js, app: app}, nil
}

// Connection failures may quote credentials in either the endpoint or an SDK
// error (notably malformed URLs). Keep diagnostics safe while retaining the
// original cause for errors.Is and errors.AsType.
type connectionError struct{ cause error }

func (connectionError) Error() string   { return "events: connect to NATS failed" }
func (e connectionError) Unwrap() error { return e.cause }

// wantedStream is the stream this code asks for, in one place, because the
// creation above and the reconciliation below have to ask for the same thing.
//
// Retention is by age: the outbox is the durable copy and the stream is the
// delivery path, so a message that has been delivered is not something this
// stream has to keep. Storage is on disk, because a restart that lost the
// undelivered ones would be a restart that lost events.
func wantedStream() *nats.StreamConfig {
	return &nats.StreamConfig{
		Name:      stream,
		Subjects:  []string{appname.SubjectSpace()},
		Retention: nats.LimitsPolicy,
		Storage:   nats.FileStorage,
		MaxAge:    delivery.Keep,
	}
}

type jetstream struct {
	nc *nats.Conn
	js nats.JetStreamContext

	// app is the slug this process publishes and subscribes as. It is in the
	// connection name an operator reads, in the address every event is published
	// at, in the filter set every consumer is created with, and in the durable
	// names it carries — see kit/appname. The empty Name is the deployment of one
	// app, which publishes and reads the addresses this kernel formed before
	// decision 0074.
	app appname.Name
}

func (j *jetstream) Publish(ctx context.Context, ev transport.Event) error {
	// The envelope's app is the publisher's own fact, stamped here rather than read
	// from the outbox: the outbox holds no app column and needs none, because the
	// one process that could fill it in already knows what it is (house rule 7).
	// An event that arrives naming another app is a relay wired to the wrong
	// composition, and the publish is where that has to say itself: writing the
	// document anyway would produce a message whose address and whose claim disagree,
	// which is the pair this envelope exists to be able to check.
	if ev.App.Named() && ev.App != j.app {
		return fmt.Errorf("events: refusing to publish %s as app %s from an app %s process", ev.Name, ev.App, j.app)
	}
	ev.App = j.app
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("events: marshal %s: %w", ev.Name, err)
	}
	// The address is the envelope's own subject attribute, from the one
	// function that writes both: a bridge that reads a subject out of a document
	// and subscribes to it lands on the messages it came from.
	if _, err := j.js.Publish(transport.AppSubject(j.app, ev.TenantID, ev.Name), body, nats.Context(ctx)); err != nil {
		return fmt.Errorf("events: publish %s: %w", ev.Name, err)
	}
	return nil
}

// ackWait is how long JetStream waits for an acknowledgement before it decides a delivery
// was lost, and it is the first rung of the redelivery ladder rather than a number beside
// it.
//
// The server pins a consumer's AckWait to BackOff[0] whenever a backoff is set, so a
// subscription that asked for a different wait was stored with the ladder's and nats.go
// refuses a later subscription whose stored configuration is not the one it asks for — a
// consumer the first process created and no later process could bind to. One value used
// twice cannot drift; reconcile handles the drift still possible, a deploy that changes the
// ladder itself.
func ackWait() time.Duration { return delivery.Backoff[0] }

// Handler attempts are capped in Subscribe. Unlimited broker redelivery keeps
// terminal-record failures recoverable; consumer backoff bounds the retry rate.
// wanted is the consumer this code asks for. It is one value because the
// subscription below and reconcile have to ask for the same thing: two lists of
// the same settings is how a consumer comes to differ from the code that
// created it.
func wanted(app appname.Name, durable, name string) []nats.SubOpt {
	return []nats.SubOpt{
		nats.Durable(durable), nats.ManualAck(), nats.AckExplicit(), nats.DeliverAll(),
		nats.AckWait(ackWait()), nats.MaxDeliver(-1), nats.BackOff(delivery.Backoff),
		nats.BindStream(stream),
		// Both addresses, while a publisher on the previous build is still
		// writing events this worker reads. See transport.Filters.
		nats.ConsumerFilterSubjects(transport.AppFilters(app, name)...),
	}
}

// group is the deliver group a durable's subscribers join, and it is the durable's own
// name.
//
// A push consumer with no deliver group belongs to one subscriber and refuses the next, so
// the second replica crashlooped on its first subscription. With a group JetStream delivers
// each message to one member of it: N workers share one durable, each event is handled
// once, and the acknowledgement ladder stays the consumer's. A queue group and not a
// durable per replica, because a durable per replica leaves a consumer behind that nothing
// drains when the replica goes.
func group(durable string) string { return durable }

// watching is the consumer one observation asks for: ephemeral, deliver-new, and filtered
// to the one subject js.Subscribe was handed. It shares nothing with wanted and takes no
// name reconcile could compare against — a stored-consumer comparison would delete an
// ephemeral consumer — no ladder to climb and no position to resume.
//
// DeliverNew and AckNone are named rather than left to the library's DeliverAll and
// AckExplicit, one each: a consumer that replayed the stream would hand a browser events
// this method's contract says it never sees, and a consumer that acknowledges has the
// server restart the ack window of every unanswered message, so a slow callback queue can
// be handed the same publish twice. The filter is the subscribe subject, which is why
// Observe passes the exact address rather than a wildcard.
func watching() []nats.SubOpt {
	return []nats.SubOpt{nats.BindStream(stream), nats.DeliverNew(), nats.AckNone()}
}

// stream brings the stored stream into line with what this code asks for, the way reconcile
// does for a consumer, because a stream outlives every process that connected and its
// settings are a second copy nothing kept in step.
//
// What differs is the answer to a setting NATS cannot change. A consumer is deleted and
// made again, replaying DeliverAll; a stream cannot be, because deleting one throws away
// the messages the outbox has already marked as relayed. So retention and storage are
// reported and the process refuses to start: a deploy that fails where somebody is looking,
// rather than a fleet on the wrong stream.
func (j *jetstream) stream(ctx context.Context) error {
	info, err := j.js.StreamInfo(stream, nats.Context(ctx))
	switch {
	case errors.Is(err, nats.ErrStreamNotFound):
		if _, err := j.js.AddStream(wantedStream(), nats.Context(ctx)); err != nil {
			return fmt.Errorf("create the %s stream: %w", stream, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("read the %s stream: %w", stream, err)
	}

	want := wantedStream()
	var changed, immutable []string
	if !slices.Equal(info.Config.Subjects, want.Subjects) {
		changed = append(changed, fmt.Sprintf("subjects %v to %v", info.Config.Subjects, want.Subjects))
	}
	if info.Config.MaxAge != want.MaxAge {
		changed = append(changed, fmt.Sprintf("max_age %s to %s", info.Config.MaxAge, want.MaxAge))
	}
	if info.Config.Retention != want.Retention {
		immutable = append(immutable, fmt.Sprintf("retention %s to %s", info.Config.Retention, want.Retention))
	}
	if info.Config.Storage != want.Storage {
		immutable = append(immutable, fmt.Sprintf("storage %s to %s", info.Config.Storage, want.Storage))
	}
	if len(immutable) > 0 {
		return fmt.Errorf("the %s stream differs in a setting NATS cannot change (%s); recreating a stream throws away the messages in it, so this is an operator's decision and not this process's",
			stream, strings.Join(immutable, ", "))
	}
	if len(changed) > 0 {
		slog.InfoContext(ctx, "events: reconciling the stored stream with this build",
			"stream", stream, "fields", changed)
		if _, err := j.js.UpdateStream(want, nats.Context(ctx)); err != nil {
			return fmt.Errorf("update the drifted stream: %w", err)
		}
	}
	return nil
}

// reconcile brings a stored consumer into line with what this code asks for, before the
// subscription that would otherwise be refused for the difference. A durable consumer
// outlives the process that made it, so its settings are a second copy of three constants,
// and a deploy that changed one used to mean a worker that could not boot until somebody
// deleted the consumer by hand.
//
// Deleting is safe because this code asks for DeliverAll: a recreated consumer replays the
// stream, and every replay is claimed in platformkit_handled before the handler runs
// (docs/adr/0004). Independent sinks must supply their own durable idempotency; this
// provider does not create SQL claims.
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
	if info.Config.MaxDeliver != -1 {
		changed = append(changed, fmt.Sprintf("max_deliver %d to %d", info.Config.MaxDeliver, -1))
		want.MaxDeliver = -1
	}
	if !slices.Equal(info.Config.BackOff, delivery.Backoff) {
		changed = append(changed, fmt.Sprintf("backoff %v to %v", info.Config.BackOff, delivery.Backoff))
		want.BackOff = delivery.Backoff
	}
	// The ones an update cannot carry: what a consumer filters, how it
	// acknowledges, where it starts, and whether it is pushed at all. A
	// consumer that differs in any of them is not this subscription's consumer
	// wearing the wrong settings, it is somebody else's under the same name.
	//
	// The filter is a set now, and a consumer stored with one filter subject is
	// a consumer that cannot see the previous build's traffic: an update that
	// cannot add the second one is the drift, so it is named as the immutable
	// kind and the consumer is remade. Remaking it replays the stream under
	// DeliverAll, and platformkit_handled claims make the replay safe — the same
	// argument two settings above.
	if want := transport.AppFilters(j.app, name); info.Config.FilterSubject != "" || !slices.Equal(info.Config.FilterSubjects, want) {
		immutable = append(immutable, fmt.Sprintf("filter_subject %q filters %v to %v",
			info.Config.FilterSubject, info.Config.FilterSubjects, want))
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
	// And the one this release added. A consumer stored by a build before it
	// has no deliver group, so it takes one subscriber and refuses the second;
	// NATS will not put a group on a live push consumer, so the consumer is
	// remade and every worker that boots after this deploy finds one it can
	// share.
	if info.Config.DeliverGroup != group(durable) {
		immutable = append(immutable, fmt.Sprintf("deliver_group %q to %q", info.Config.DeliverGroup, group(durable)))
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

func (j *jetstream) Subscribe(ctx context.Context, durable, name string, sink transport.Sink) error {
	if err := j.stream(ctx); err != nil {
		return fmt.Errorf("events: subscribe %s to %s: %w", durable, name, err)
	}
	if err := j.reconcile(ctx, durable, name); err != nil {
		return fmt.Errorf("events: subscribe %s to %s: %w", durable, name, err)
	}
	// No subscribe subject: a consumer with more than one filter is not
	// addressable through the subject-based API — nats.go asks the server for a
	// consumer whose filter is the subject it was handed, and the server refuses
	// one with both — so the subscription binds to the durable and the consumer
	// owns its filter set, from wanted above and from nowhere else.
	sub, err := j.js.QueueSubscribe("", group(durable), func(msg *nats.Msg) {
		var ev transport.Event
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			// A message that will never parse would be redelivered forever.
			// Terminate it and say so; the outbox still holds the row.
			slog.ErrorContext(ctx, "events: undecodable message", "subject", msg.Subject, "error", err)
			_ = msg.Term()
			return
		}
		// The address this message arrived on is a fact the broker knows and the
		// document does not get to decide alone. The consumer's filter fixes the
		// event's name and leaves the tenant as a wildcard, and events.Consume
		// opens the handler's transaction in the tenant the document names, so a
		// message stored on one tenant's address and stamped as another's would
		// run its handler inside the second tenant's rows. Terminating it is the
		// same shape as the branch above: the copy is undeliverable as addressed,
		// the outbox still holds the row, and the correctly addressed copy — the
		// one the relay wrote — is a different message on a different subject.
		if err := transport.AddressMismatch(j.app, msg.Subject, ev); err != nil {
			slog.ErrorContext(ctx, "events: message at an address its document does not claim",
				"subject", msg.Subject, "event", ev.Name, "id", ev.ID, "tenant", ev.TenantID, "error", err)
			_ = msg.Term()
			return
		}
		meta, err := msg.Metadata()
		if err != nil {
			slog.ErrorContext(ctx, "events: delivery metadata unavailable", "error", err)
			return
		}
		// Broker retries remain available after the handler cap, including across
		// restarts. Those deliveries retry only terminal persistence, never the
		// provider action. The last handler cause is logged before recording; a
		// restarted delivery may no longer have it.
		err = errors.New("delivery attempts exhausted; last handler error unavailable on redelivery")
		if meta.NumDelivered <= uint64(delivery.MaxDeliveries) {
			err = sink.Handle(ctx, ev)
			if err == nil {
				_ = msg.Ack()
				return
			}
		}
		if meta.NumDelivered >= uint64(delivery.MaxDeliveries) {
			slog.ErrorContext(ctx, "events: recording terminal failure", "event", ev.Name, "id", ev.ID, "error", err)
			if err = sink.Dead(ctx, ev, err); err == nil {
				_ = msg.Term()
				return
			}
		}
		wait := delivery.Backoff[min(meta.NumDelivered, uint64(len(delivery.Backoff)))-1]
		slog.WarnContext(ctx, "events: delivery unfinished, retrying",
			"event", ev.Name, "id", ev.ID, "backoff", wait, "error", err)
		// Leave the message pending: the consumer already owns its retry timer.
		// Combining BackOff with NakWithDelay compounds later rungs on NATS 2.14.6.
	}, wanted(j.app, durable, name)...)
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

// Observe watches one tenant's one event from now on, on an ephemeral consumer the server
// names and deletes when this connection's subscription goes away. It asks for no durable
// name, no ack ladder and no stored position, so it can create, update or delete nothing a
// durable subscription owns.
//
// No bound is set here: the queue that decides whether a connection is alive is
// kit/stream's per-connection one, and a subscriber that cannot keep up is dropped by that
// bound rather than by a server redelivering to a listener that stopped caring.
func (j *jetstream) Observe(ctx context.Context, tenantID uuid.UUID, name string, fn func(transport.Event)) (func(), error) {
	// The stream has to exist to bind to; nothing else is prepared, and no
	// reconciliation runs, because an observation owns no stored consumer.
	if err := j.stream(ctx); err != nil {
		return nil, fmt.Errorf("events: observe %s in tenant %s: %w", name, tenantID, err)
	}
	subject := transport.AppSubject(j.app, tenantID, name)
	sub, err := j.js.Subscribe(subject, func(msg *nats.Msg) {
		var ev transport.Event
		// An undecodable body is dropped, which is the whole of the handling it gets:
		// this consumer acknowledges nothing, so nothing comes back to be read again.
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			slog.ErrorContext(ctx, "events: undecodable message", "subject", msg.Subject, "error", err)
			return
		}
		// The filter fixed the name and the tenant, because this is an exact subject
		// rather than a wildcard — but the filter is what the broker routed by, not
		// what the document claims, and the two are still both read. A message written
		// at this address by a publisher claiming another app, or no app at all from a
		// process that names one, is somebody else's delivery.
		if err := transport.AddressMismatch(j.app, msg.Subject, ev); err != nil {
			slog.ErrorContext(ctx, "events: message at an address its document does not claim",
				"subject", msg.Subject, "event", ev.Name, "id", ev.ID, "tenant", ev.TenantID, "error", err)
			return
		}
		// Nothing is acknowledged and nothing is asked for: an observation dropped
		// here is a notice the client is told to refetch rather than one this process
		// owes, and an ack would be the broker's licence to deliver it a second time.
		defer func() {
			if v := recover(); v != nil {
				slog.ErrorContext(ctx, "events: an observation panicked; the notice was dropped",
					"event", ev.Name, "id", ev.ID, "tenant", ev.TenantID, "panic", v)
			}
		}()
		fn(ev)
	}, watching()...)
	if err != nil {
		return nil, fmt.Errorf("events: observe %s in tenant %s: %w", name, tenantID, err)
	}
	var once sync.Once
	stop := func() {
		// Unsubscribe and not Drain: draining waits for in-flight messages, and a
		// withdrawal the caller already has nothing to answer for never blocks on a
		// delivery. The error is the server's about a consumer it is already
		// deleting; Unsubscribe also deletes the ephemeral consumer with it.
		once.Do(func() { _ = sub.Unsubscribe() })
	}
	go func() {
		<-ctx.Done()
		stop()
	}()
	return stop, nil
}

// Close releases the connection. It is not part of transport.Transport, because the
// memory transport has nothing to release; kit/app asks for io.Closer.
func (j *jetstream) Close() error {
	j.nc.Close()
	return nil
}
