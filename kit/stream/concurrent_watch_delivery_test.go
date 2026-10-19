package stream

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/module"
)

// openingTransport pauses each Observe after the real provider has registered
// it, allowing a publish while concurrent callers are still joining the hub.
type openingTransport struct {
	transport.Transport
	calls atomic.Int32
	ready chan int
	allow [2]chan struct{}
}

func (o *openingTransport) Observe(ctx context.Context, tenant uuid.UUID, name string, fn func(transport.Event)) (func(), error) {
	n := int(o.calls.Add(1)) - 1
	stop, err := o.Transport.Observe(ctx, tenant, name, fn)
	if err != nil {
		return stop, err
	}
	o.ready <- n
	<-o.allow[n]
	return stop, nil
}

func TestConcurrentWatchOpeningsDeliverEachNoticeOnce(t *testing.T) {
	f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: module.StreamAnyPrincipal()}}, slowHeartbeat())
	tr := &openingTransport{Transport: memory.New(), ready: make(chan int, 2),
		allow: [2]chan struct{}{make(chan struct{}), make(chan struct{})}}
	f.hub.transport = tr
	topic, _ := f.hub.registry.Topic("task.closed")
	connections := make([]*connection, 2)
	finished := make(chan error, 2)
	for i := range connections {
		c, err := f.hub.register(f.request(0, i, topic.Event), []Topic{topic})
		if err != nil {
			t.Fatal(err)
		}
		connections[i] = c
		go func() { finished <- f.hub.watchTopic(t.Context(), c, topic) }()
	}
	select {
	case <-tr.ready:
	case <-time.After(time.Second):
		t.Fatal("the watch never reached the provider")
	}
	// A hub that serializes opening needs only one provider call. If it starts
	// another, keep that observation in flight through the first publication.
	select {
	case <-tr.ready:
	case <-time.After(100 * time.Millisecond):
	}
	close(tr.allow[0])
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	ev := event(t, topic.Event, f.tenants[0].ID, `{}`)
	if err := tr.Publish(t.Context(), ev); err != nil {
		t.Fatal(err)
	}
	close(tr.allow[1])
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	for _, c := range connections {
		defer f.hub.release(c)
		if got := len(c.notices); got > 1 {
			t.Errorf("one publish queued %d notices for one connection during concurrent watch openings; want at most 1", got)
		}
	}
}
