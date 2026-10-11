package events

// REVIEW round 2 (T-0109). The pin of the cure for finding 3, at the edge the
// cure itself introduced.
//
// The window is carried by a consumer that filters *two* subjects, and
// `reconcile` (kit/events/providers/nats/jetstream.go) decides whether a stored
// consumer has drifted with:
//
//	if want := transport.Filters(name); info.Config.FilterSubject != "" || !slices.Equal(info.Config.FilterSubjects, want)
//
// and a consumer that fails that test is deleted and made again — "a log line
// rather than an operator's afternoon", because DeliverAll re-reads the stream
// and platformkit_handled claims make the replay harmless.
//
// `slices.Equal` is ordered. `transport.Filters` returns its two addresses in
// one order and the server is what answers `info.Config.FilterSubjects`; the
// comparison is between a list this process wrote and a list the broker stored,
// and nothing in this repository pins which order comes back. If the broker ever
// canonicalises the set — sorted, deduplicated, grouped — then every subscription
// on every boot is "immutable drift", every durable is deleted, and the whole
// stream is re-delivered on every restart of every worker, forever, rather than
// once at the upgrade. That is not a drift being reconciled; it is a mechanism
// that never stops reconciling, and the idempotency argument that makes one
// re-delivery cheap does not make an infinite one cheap.
//
// So the second boot has to find the consumer it made, not make a new one. The
// assertion reads the consumer's own creation instant — the field that moves when
// a consumer is deleted and created — rather than a log line about it.

import (
	"context"
	"slices"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/septagon-oss/platformkit/kit/events/transport"
)

func TestTheNextBootFindsTheConsumerThisBuildMade(t *testing.T) {
	ctx := t.Context()
	first, js := jetstreamForTest(t)
	durable, name := uniqueDurable(t)

	sink := Sink{
		Handle: func(context.Context, Event) error { return nil },
		Dead:   func(context.Context, Event, error) error { return nil },
	}
	if err := first.Subscribe(ctx, durable, name, sink); err != nil {
		t.Fatalf("first boot subscribe: %v", err)
	}
	made, err := js.ConsumerInfo(stream, durable, nats.Context(ctx))
	if err != nil {
		t.Fatalf("read the consumer the first boot made: %v", err)
	}
	if made.Config.FilterSubject != "" {
		t.Fatalf("the consumer was stored with one filter subject %q, so the window's second address is nowhere to be read",
			made.Config.FilterSubject)
	}
	if !slices.Equal(made.Config.FilterSubjects, transport.Filters(name)) {
		t.Errorf("the broker stored %v where this build asked for %v — the set the code compares against is not the set the broker keeps, "+
			"so reconcile's slices.Equal refuses it on every boot", made.Config.FilterSubjects, transport.Filters(name))
	}

	// A second boot: a new process, a new connection, the same durable. This is
	// the restart, and the second replica the deliver group exists for.
	second, _ := jetstreamForTest(t)
	if err := second.Subscribe(ctx, durable, name, sink); err != nil {
		t.Errorf("the second boot could not bind to the stored consumer: %v", err)
	}
	after, err := js.ConsumerInfo(stream, durable, nats.Context(ctx))
	if err != nil {
		t.Fatalf("read the consumer after the second boot: %v", err)
	}
	if !after.Created.Equal(made.Created) {
		t.Errorf("the second boot deleted and re-made the durable: created %s, now %s. reconcile re-delivers the whole "+
			"stream under DeliverAll on every such remake, so a filter set it cannot read back unchanged means a worker that "+
			"replays the stream each time it starts", made.Created, after.Created)
	}
	if !slices.Equal(after.Config.FilterSubjects, transport.Filters(name)) {
		t.Errorf("after the second boot the durable filters %v, want %v", after.Config.FilterSubjects, transport.Filters(name))
	}
}
