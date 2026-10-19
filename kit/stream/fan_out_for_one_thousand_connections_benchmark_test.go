package stream

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// BenchmarkFanOutTimePerNoticeForOneThousandConnections measures what one publish
// costs when a thousand live connections of one tenant watch the same
// (tenant, event): the snapshot taken under the hub's mutex, the recipient filter
// run per connection, and the send into a thousand queues. The queues are drained
// in the measured loop, so the number is the fan-out itself — no frame bytes, no
// response writer, no broker round trip — which is the part that decides whether a
// busy tenant's connections receive notices or receive a resync.
//
// It asserts no threshold, and says so rather than leaving the absence to be
// discovered: a ceiling chosen before anything was measured protects nobody, and
// this path already has the bound it will defend — the queue's, reported to the
// connection as one overflow resync. The reading belongs beside the host it was
// taken on, in the report of the round that ran it.
func BenchmarkFanOutTimePerNoticeForOneThousandConnections(b *testing.B) {
	const connections = 1000
	limits := DefaultLimits()
	limits.MaxPerTenant = connections
	limits.Queue = connections
	f := &fixture{transport: newFakeTransport(), authority: &fakeAuthority{renewal: Renewal{SignedIn: true}}}
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "alpha"}
	f.hub = New(Deps{
		Transport: f.transport,
		Registry: NewRegistry([]module.Module{{Name: "task", Events: []string{"task.closed"},
			Streams: []module.Stream{{Event: "task.closed", Policy: module.StreamAnyPrincipal()}}}}),
		Authority: f.authority,
		Limits:    limits,
		Clock:     fixedClock{at: time.Date(2026, 10, 14, 9, 40, 0, 0, time.UTC)},
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	topic, ok := f.hub.registry.Topic("task.closed")
	if !ok {
		b.Fatal("the benchmark's own registry does not declare its topic")
	}

	// A thousand people, not one: the per-person ceiling is a count of one
	// person's tabs, and a thousand tabs of one account is not what a tenant at
	// this size looks like.
	key := watchKey{tenant: tenant.ID, event: topic.Event}
	held := make([]*connection, 0, connections)
	for range connections {
		c, err := f.hub.register(Request{Tenant: tenant, Principal: tenancy.Principal{UserID: uuid.New()}}, []Topic{topic})
		if err != nil {
			b.Fatalf("connection %d of the ceiling this benchmark names was refused: %v", len(held), err)
		}
		held = append(held, c)
	}
	f.hub.watches[key] = &watch{stop: func() {}, conns: map[*connection]struct{}{}}
	for _, c := range held {
		f.hub.watches[key].conns[c] = struct{}{}
	}

	ev := transport.Event{ID: uuid.New(), Name: topic.Event, TenantID: tenant.ID,
		Payload: []byte(`{}`), At: time.Date(2026, 10, 14, 9, 41, 0, 0, time.UTC)}
	received := 0
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		f.hub.deliver(key, ev)
		for _, c := range held {
			select {
			case n := <-c.notices:
				received++
				_ = n
			default:
				b.Fatalf("a connection received nothing from the publish; %d of %d arrived", received, connections)
			}
		}
	}
	b.StopTimer()
	if got := received; got != b.N*connections {
		b.Fatalf("%d notices arrived for %d publishes to %d connections", got, b.N, connections)
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(max(b.N, 1))/connections, "ns/connection")
	b.ReportMetric(float64(received)/float64(max(b.Elapsed().Seconds(), 1e-9)), "notices/s")
}
