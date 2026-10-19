package stream

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/module"
)

func TestADroppedTopicCannotDeliverItsQueuedNotices(t *testing.T) {
	f := newFixture(t, []module.Stream{
		{Event: "task.closed", Policy: module.StreamPermission("task:read")},
		{Event: "task.opened", Policy: module.StreamAnyPrincipal()},
	}, slowHeartbeat())
	closed, _ := f.hub.registry.Topic("task.closed")
	opened, _ := f.hub.registry.Topic("task.opened")
	c, err := f.hub.register(f.request(0, 0, closed.Event, opened.Event), []Topic{closed, opened})
	if err != nil {
		t.Fatal(err)
	}
	defer f.hub.release(c)
	for _, topic := range []Topic{closed, opened} {
		if err := f.hub.watchTopic(t.Context(), c, topic); err != nil {
			t.Fatal(err)
		}
	}
	ev := event(t, closed.Event, f.tenants[0].ID, `{}`)
	f.transport.send(f.tenants[0].ID, ev)
	f.authority.answer(Renewal{SignedIn: true, Refused: []string{"task:read"}})
	w := newRecorder()
	rc := http.NewResponseController(w)
	if err := f.hub.beat(t.Context(), rc, w, c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.written(), "event: dropped") {
		t.Fatal("heartbeat did not withdraw the topic")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := f.hub.loop(ctx, rc, w, c); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.written(), "id: "+ev.ID.String()) {
		t.Errorf("a notice reached the connection after its topic was dropped:\n%s", w.written())
	}
}
