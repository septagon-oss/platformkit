package app

// The durable consumer name is the one shared name that says which app owns a
// delivery: it is the JetStream consumer name on the single PLATFORMKIT stream,
// the queue group every replica of that app joins, and half the primary key of
// platformkit_handled and platformkit_dead_letters. Two compositions over one
// broker that name one module and one event therefore name one consumer, one
// queue group and one handled-ledger key — and the second of them to bind either
// fails to bind or shares the first's backlog, which is one app load-balancing
// another app's tenants' work into its own handlers.
//
// kit/appname.Durable takes the app, and events.Subscription has the field it
// reads. What has to hold is that the composition that names an app puts that
// slug into every subscription it hands the transport — the same fact
// Options.App already carries to the job lock and to the relay's claim. This is
// that claim, read off the worker the composition actually runs.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
)

// consumerNames is a transport that keeps the consumer name each subscription is
// created under. Subscribe is the moment a durable reaches the broker, so it is
// where the composition's app either is in the name or is not.
type consumerNames struct {
	mu    sync.Mutex
	got   []string
	calls chan struct{}
}

func (c *consumerNames) Publish(context.Context, events.Event) error { return nil }

func (c *consumerNames) Subscribe(_ context.Context, durable, name string, _ events.Sink) error {
	c.mu.Lock()
	c.got = append(c.got, durable+"\x00"+name)
	c.mu.Unlock()
	select {
	case c.calls <- struct{}{}:
	default:
	}
	return nil
}

func (c *consumerNames) names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.got...)
}

func TestACompositionThatNamesItsAppNamesEveryConsumerForThatApp(t *testing.T) {
	cfg, opts := compose(t)
	opts.Role = Worker
	opts.App = appname.MustParse("acme")
	rec := &consumerNames{calls: make(chan struct{}, 8)}
	opts.Transport = rec

	// One module, one event, one subscription: the shape two compositions of one
	// database share when both compose the same module.
	billing := module.Module{
		Name:   "billing",
		Events: []string{"billing.plan.created"},
		Subscriptions: []events.Subscription{{
			Module: "billing", Name: "billing.plan.created",
			Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error { return nil },
		}},
	}

	a, err := New(t.Context(), cfg, []module.Module{billing, brand("all", "unused")}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rt, err := a.Start(t.Context())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- rt.Work(ctx) }()

	deadline := time.After(10 * time.Second)
	for len(rec.names()) == 0 {
		select {
		case <-rec.calls:
		case <-deadline:
			t.Fatal("the worker never subscribed the composed module's event")
		}
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Work did not return when its context was done")
	}

	want := appname.Durable("acme", "billing", "billing.plan.created")
	var seen []string
	for _, entry := range rec.names() {
		durable, name, _ := strings.Cut(entry, "\x00")
		if name != "billing.plan.created" {
			continue
		}
		seen = append(seen, durable)
		if durable != want {
			t.Errorf("the composition whose app is %q subscribed %s as consumer %q: the durable is the name that says which app owns this delivery, and a second app composing the same module names the same consumer",
				"acme", name, durable)
		}
	}
	if len(seen) == 0 {
		t.Fatalf("no consumer was named for billing.plan.created; the worker subscribed %v", rec.names())
	}
}
