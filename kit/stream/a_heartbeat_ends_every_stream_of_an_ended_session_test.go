package stream

import (
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/module"
)

// TestAHeartbeatEndsEveryStreamOfAnEndedSession checks that the session is asked
// again at the heartbeat whatever policy the connection's topics carry: a person
// who signed out, or was removed from the tenant, stops receiving any-principal and
// recipient notices too, not only permission ones.
func TestAHeartbeatEndsEveryStreamOfAnEndedSession(t *testing.T) {
	for name, policy := range map[string]module.StreamPolicy{
		"any principal": module.StreamAnyPrincipal(),
		"recipient":     module.StreamRecipient("assignee"),
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: policy}}, fastHeartbeat())
			c := f.open(t, f.request(0, 0, "task.closed"))
			defer c.stop()
			c.await(t, ": ping")
			f.authority.answer(Renewal{SignedIn: false})
			select {
			case <-c.done:
			case <-time.After(2 * time.Second):
				t.Fatalf("a %s stream outlived its session; it went on writing heartbeats (%d in all)",
					name, strings.Count(c.w.written(), ": ping"))
			}
		})
	}
}
