package events

// REVIEW round 1 (T-0109), finding 3.
//
// transport/cloudevents.go justifies accepting the pre-envelope shape with one
// argument, and kit/events/README.md turns it into an instruction: "while a
// publisher on the previous build is writing events a new worker reads, refusing
// those messages would terminate events the relay had already stamped. Deploy
// the consumers onto this build before any publisher."
//
// The previous build published at `platformkit.<name>`; this build's
// subscription filters `platformkit.*.<name>`, and a `*` matches exactly one
// subject token, so a three-token address never matches a four-token filter.
// The order the README gives is therefore the order that loses the window's
// events: the old publisher's message is accepted by the stream
// (`platformkit.>`), matched by no consumer, and its outbox row is stamped
// published_at by a relay that saw Publish return nil — not pending, not
// dead-lettered, not delayed. The decoder that exists for the window can never
// be reached through the broker, in either order.
//
// The assertion below is the promise, not the accident: a message this package
// says it reads, arriving at the address the previous build used, reaches the
// subscription this build made for that event. It passes if the window is given
// a real mechanism for its length (subscribe both addresses while it is open, or
// a stream subject transform), and it passes equally if the previous build's
// traffic is drained before any consumer moves — provided the code, the README
// and the decoder agree on which. If the answer is "there is no window", then
// the pre-envelope branch of UnmarshalJSON goes with the claim, and this test
// with it; what cannot stand is all three as they are.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAPreEnvelopeMessageReachesTheSubscriptionThatClaimsToReadIt(t *testing.T) {
	transport, js := jetstreamForTest(t)
	durable, name := uniqueDurable(t)
	tenantID, actor := uuid.New(), uuid.New()

	seen := make(chan Event, 4)
	if err := transport.Subscribe(t.Context(), durable, name, Sink{
		Handle: func(_ context.Context, ev Event) error { seen <- ev; return nil },
		Dead:   func(context.Context, Event, error) error { return nil },
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// The control, so the failure below can only be about the address: a
	// message this build publishes arrives, on the tenant-segmented subject,
	// and the sink is alive.
	control := Event{ID: uuid.New(), Name: name, TenantID: tenantID, Payload: []byte(`{"amount":42}`)}
	if err := transport.Publish(t.Context(), control); err != nil {
		t.Fatalf("publish the control: %v", err)
	}
	select {
	case got := <-seen:
		if got.ID != control.ID {
			t.Fatalf("the sink delivered %s, want the control %s", got.ID, control.ID)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the subscription received nothing even from this build's own publish")
	}

	// The previous build's message: its address and the exact body its relay
	// wrote. Pinned first, because the loss this test is about is the filter's,
	// not the decoder's — if this body ever stops decoding, the finding below
	// is a different one and this test should say so.
	legacy := fmt.Sprintf(
		`{"id":%q,"name":%q,"tenantId":%q,"payload":{"amount":7},"at":"2026-09-28T00:00:00Z","actor":%q}`,
		uuid.NewString(), name, tenantID.String(), actor.String())
	var want Event
	if err := json.Unmarshal([]byte(legacy), &want); err != nil {
		t.Fatalf("the pre-envelope body no longer reads, so this case proves nothing: %v", err)
	}
	if _, err := js.Publish(subject+name, []byte(legacy)); err != nil {
		t.Fatalf("publish at the previous build's address: %v", err)
	}

	select {
	case got := <-seen:
		if got.ID != want.ID {
			t.Errorf("the sink delivered %s, want the pre-envelope event %s", got.ID, want.ID)
		}
	case <-time.After(30 * time.Second):
		t.Errorf("the pre-envelope event %s, published at %s, never reached the subscription filtering %q: "+
			"the stream kept it, its outbox row was stamped published, and nothing handled it",
			want.ID, subject+name, "platformkit.*."+name)
	}
}
