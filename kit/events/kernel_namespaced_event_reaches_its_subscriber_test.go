package events

// A kernel-namespaced event reaches the subscriber its address names.
//
// The composition has an event whose namespace is not the name of
// the manifest that emits it: `security.denied` is declared by the kernel's
// manifest, whose name is `platformkit` (kit/module.KernelName), and is listed
// in module.KernelEvents. That is the one exemption the merged tree needed, and
// it puts a new shape on the wire the address scheme never carried before: the
// third subject token of a live event is now a namespace (`security`) that is
// not the emitter, while the emitter's own name (`platformkit`) is the stream's
// first token.
//
// Everything downstream of that choice is a string built by transport.Subject
// and transport.Filter, and the only existing broker-level case relays an event
// of the `ledger_xxx.happened` shape, where the namespace and the emitter would
// agree either way. So nothing in this repository currently proves that the
// refusal event a real deployment emits most often is delivered at all: the
// composition-level proof in apps/platformkit drives the memory transport,
// which routes by name and never looks at a subject.
//
// This is the case that does — the same question the brief asks of every event,
// asked of the one event the kernel emits by a door no other event uses. It
// fails if Subject or Filter ever derives its module token from the emitting
// manifest rather than from the event's own name, which is the change that
// would put every refusal into a subject no consumer filters, silently, with
// the outbox row stamped published.

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAKernelNamespacedEventReachesTheSubscriptionOnItsTenantsAddress(t *testing.T) {
	broker, js := jetstreamForTest(t)
	_, conn := dbtest.Schema(t)

	const name = "security.denied"
	durable := "kernel_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	t.Cleanup(func() {
		nc, err := nats.Connect(os.Getenv("PLATFORMKIT_TEST_NATS_URL"))
		if err != nil {
			return
		}
		defer nc.Close()
		if js, err := nc.JetStream(); err == nil {
			_ = js.DeleteConsumer(stream, durable)
		}
	})

	seen := make(chan Event, 64)
	if err := broker.Subscribe(t.Context(), durable, name, Sink{
		Handle: func(_ context.Context, ev Event) error { seen <- ev; return nil },
		Dead:   func(context.Context, Event, error) error { return nil },
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	actor := uuid.New()
	// Written the way kit/app's denial hook writes it: the refused request's own
	// transaction, the tenant resolved from that request, the actor off the
	// context. The payload is the denial's fields, untyped here on purpose — the
	// declaration that types it lives in kit/app, above this package.
	err := db.Run(tenancy.WithActor(tenancy.WithTenant(t.Context(), tenant), actor), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return Publish(ctx, tx, name, map[string]any{
				"status": 403, "code": "AUTH_DENIED", "detail": "AUTH_DENIED: this operation requires task:resolve",
				"method": "POST", "path": "/api/v1/tasks/t-1/resolve", "userId": uuid.NewString(),
			})
		})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := Relay(t.Context(), conn, broker); err != nil {
		t.Fatalf("relay: %v", err)
	}

	subject := transport.Subject(tenant.ID, name)
	msg, err := js.GetLastMsg(stream, subject)
	if err != nil {
		t.Fatalf("nothing was stored under %q: %v", subject, err)
	}
	if got := len(strings.Split(subject, ".")); got != 4 {
		t.Errorf("the address %q has %d tokens, want 4: the tenant segment is what makes a per-tenant durable and a per-tenant bridge possible, and this event is the first whose namespace is not its emitter's name", subject, got)
	}

	var doc map[string]any
	if err := json.Unmarshal(msg.Data, &doc); err != nil {
		t.Fatalf("the relayed body is not JSON: %v (%s)", err, msg.Data)
	}
	for attr, want := range map[string]string{
		"specversion":     "1.0",
		"type":            name,
		"subject":         subject,
		"datacontenttype": "application/json",
		"tenantid":        tenant.ID.String(),
	} {
		if got := doc[attr]; got != want {
			t.Errorf("%s = %v, want %q", attr, got, want)
		}
	}

	// The address just read is the address the subscription to this event name
	// filters: a durable that wants every tenant's refusals and a durable that
	// wants one tenant's are both spelled from the same two functions.
	if got := transport.Filter(name); got != "platformkit.*."+name {
		t.Fatalf("Filter(%q) = %q: the wildcard this case subscribes is not the one the address matches", name, got)
	}

	// And a subscription made the way module.Expand makes one for the audit
	// module reaches it. A fresh consumer under DeliverAll can carry history for
	// this filter, so the loop looks for this event by id rather than taking the
	// first delivery.
	var got *Event
	deadline := time.After(30 * time.Second)
	for got == nil {
		select {
		case ev := <-seen:
			if ev.Name != name {
				t.Errorf("the subscription for %q delivered %q", name, ev.Name)
				continue
			}
			if ev.TenantID != tenant.ID {
				continue // somebody else's refusal, from before this run
			}
			got = &ev
		case <-deadline:
			t.Fatalf("no delivery of the %s published at %s reached the durable subscribed to %q: the audit module keeps refusals through this subscription, and a subject no filter matches is a refusal that is never audited",
				name, subject, name)
		}
	}
	if got.Actor != actor {
		t.Errorf("the delivery names actor %s, want the refusal's %s", got.Actor, actor)
	}
	var payload map[string]any
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("the delivered payload is not JSON: %v (%s)", err, got.Payload)
	}
	if payload["code"] != "AUTH_DENIED" {
		t.Errorf("the delivered payload lost its reason: %v", payload)
	}
}
