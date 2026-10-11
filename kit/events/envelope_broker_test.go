package events

// The envelope as a broker sees it. kit/events/transport asserts what this
// package marshals; this one asserts what leaves the outbox, which is the
// question the brief asks: read an event back off NATS and check it is a
// CloudEvents 1.0 envelope with the tenant in its address and the trace of the
// request that caused it.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
)

func TestARelayedEventIsACloudEventsEnvelopeOnItsTenantsSubject(t *testing.T) {
	broker, js := jetstreamForTest(t)
	admin, conn := dbtest.Schema(t)

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	name := "ledger_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "") + ".happened"
	actor := uuid.New()
	tr, ok := trace.FromRequestID(uuid.NewString())
	if !ok {
		t.Fatal("a UUID request id does not seed a trace")
	}

	// Published the way a request publishes it: the tenant from the
	// transaction, the actor and the trace from the request's context.
	req := tenancy.WithActor(tenancy.WithTenant(t.Context(), tenant), actor)
	err := db.Run(trace.With(req, tr), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return Publish(ctx, tx, name, map[string]string{"kept": "because it committed"})
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := Relay(t.Context(), conn, broker); err != nil {
		t.Fatalf("relay: %v", err)
	}

	var id uuid.UUID
	if err := admin.QueryRowContext(t.Context(), `SELECT id FROM platformkit_outbox WHERE name=$1`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}

	subject := transport.Subject(tenant.ID, name)
	msg, err := js.GetLastMsg(stream, subject)
	if err != nil {
		t.Fatalf("nothing was stored under %q: %v", subject, err)
	}

	// The envelope, as a foreign reader would decode it: the context attributes
	// by their standard names, and the tenant readable without opening the data.
	var doc map[string]any
	if err := json.Unmarshal(msg.Data, &doc); err != nil {
		t.Fatalf("the relayed body is not JSON: %v (%s)", err, msg.Data)
	}
	for attr, want := range map[string]string{
		"specversion":     "1.0",
		"id":              id.String(),
		"type":            name,
		"source":          "/" + strings.SplitN(name, ".", 2)[0],
		"subject":         subject,
		"datacontenttype": "application/json",
		"tenantid":        tenant.ID.String(),
		"actor":           actor.String(),
	} {
		if got := doc[attr]; got != want {
			t.Errorf("%s = %v, want %q", attr, got, want)
		}
	}
	if doc["traceparent"] != tr.Parent() {
		t.Errorf("traceparent = %v, want the publisher's %s", doc["traceparent"], tr.Parent())
	}
	if data, ok := doc["data"].(map[string]any); !ok || data["kept"] != "because it committed" {
		t.Errorf("data = %v", doc["data"])
	}

	// And read back by this program: the envelope carries every fact the
	// publisher had, which is what a subscriber outside the process gets.
	var ev Event
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		t.Fatalf("the envelope this relay published does not read back: %v", err)
	}
	if ev.Name != name || ev.TenantID != tenant.ID || ev.Actor != actor || ev.TraceParent != tr.Parent() {
		t.Errorf("delivery lost facts: %+v", ev)
	}

	// The address is the tenant's, and the wildcard an ordinary subscription
	// filters is the same address with the tenant opened up: one tenant's
	// durable can be exact, and every other subscription still sees its event.
	if filter := transport.Filter(name); !strings.HasPrefix(subject, "platformkit."+tenant.ID.String()+".") ||
		filter != "platformkit.*."+name {
		t.Errorf("subject %q and filter %q are not the tenant's address and its wildcard", subject, filter)
	}
}

// TestARelayedEventWithoutARequestCarriesNoTraceParent: a periodic job's event
// has no trace to inherit, and an empty attribute is a claim about one.
func TestARelayedEventWithoutARequestCarriesNoTraceParent(t *testing.T) {
	broker, js := jetstreamForTest(t)
	_, conn := dbtest.Schema(t)

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	name := "ledger_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "") + ".tick"
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return Publish(ctx, tx, name, map[string]bool{"job": true})
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := Relay(t.Context(), conn, broker); err != nil {
		t.Fatalf("relay: %v", err)
	}
	msg, err := js.GetLastMsg(stream, transport.Subject(tenant.ID, name))
	if err != nil {
		t.Fatalf("nothing was stored: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(msg.Data, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["traceparent"]; ok {
		t.Errorf("an event nobody asked for carries %v", doc["traceparent"])
	}
	if _, ok := doc["actor"]; ok {
		t.Errorf("an event nobody asked for names an actor: %v", doc["actor"])
	}
}
