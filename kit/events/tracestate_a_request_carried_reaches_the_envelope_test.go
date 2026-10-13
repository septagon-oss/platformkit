package events

// A pin, not a defect report. Nothing here fails at
// the head it was written against; it holds the assertion this branch is most
// easily able to lose without noticing.
//
// kit/events/envelope_broker_test.go reads an envelope back off NATS and checks
// every attribute the relay is supposed to carry — and of the two headers the
// W3C distributed-tracing extension names, it checks one. `traceparent` is
// asserted twice (TestARelayedEventIsACloudEventsEnvelopeOnItsTenantsSubject and
// the tenant-boundary pin), so that half of the pair cannot be dropped quietly.
// `tracestate` — the attribute kit/trace's own comment says a vendor's entry in
// is "not this program's to interpret or reorder", the attribute migration
// 000028 added a column for, and the attribute 3809d04 bounded at 512 bytes
// because the relay republishes it — is asserted by nothing in the repository:
//
// 	$ grep -rn "TraceState" --include=*_test.go kit/events kit/app apps/platformkit
// 	kit/events/transport/cloudevents_test.go:44:	ev.TraceState = "rojo=00f067aa0ba902b7"
// 	kit/events/transport/cloudevents_test.go:85:	ev.TraceState = "rojo=00f067aa0ba902b7"
//
// both of which set it on a value this package marshals in-process. Deleting the
// two lines that carry it out of the row (kit/events/relay.go:98-99) or dropping
// the `tracestate` member from the envelope (transport/cloudevents.go:86) leaves
// every existing case green: the column would keep being written, the envelope
// would keep claiming a trace it only half tells, and an integrator reading the
// document would get a traceparent with no vendor state and no way to see that
// anything went missing. That is a claim resting on a comment, which is the
// exact failure this pillar exists to remove, so it is asserted here end to end:
// through the door (trace.Parse, the one call kit/httpx/request_id.go:53 makes
// with a caller's headers), into the row, onto the wire, and back through
// transport.Event — one and the same bounded value at all four places, which is
// what 3809d04 says of its own cure ("the row, the envelope and the header can
// only ever carry the same bounded value").

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

func TestTheTracestateARequestCarriedReachesTheRowTheEnvelopeAndTheDelivery(t *testing.T) {
	broker, js := jetstreamForTest(t)
	admin, conn := dbtest.Schema(t)

	const parent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	// Four vendor entries, too wide to keep whole: this is the shape a proxy
	// with several vendors sends, and it is what net/http hands the middleware
	// that calls trace.Parse (kit/httpx/request_id.go:53) — nothing below is a
	// value an HTTP request cannot produce.
	caller := "ddt=" + strings.Repeat("x", 200) + ",scr=" + strings.Repeat("y", 200) +
		",eg=" + strings.Repeat("z", 200) + ",oo=" + strings.Repeat("w", 200)

	for _, tc := range []struct {
		name   string
		suffix string
		state  string
		// want is what the envelope attribute has to be: the caller's header
		// verbatim when it fits, and a whole-entry prefix of it when it does
		// not. Empty is never expected here — a request that carried a trace
		// state does not lose it on the way to the broker.
		want string
	}{
		{
			name:   "a vendor state that fits arrives verbatim",
			suffix: "fits",
			state:  "ddt=qZfHpR4R3pF79i4YBiyyhq2t",
			want:   "ddt=qZfHpR4R3pF79i4YBiyyhq2t",
		},
		{
			name:   "a vendor state too wide to keep arrives whole and bounded",
			suffix: "wide",
			state:  caller,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, ok := trace.Parse(parent, tc.state)
			if !ok {
				t.Fatal("trace.Parse refused a valid traceparent because of its tracestate")
			}
			want := tc.want
			if want == "" {
				want = in.TraceState
			}
			if want == "" {
				t.Fatal("the door dropped the caller's whole tracestate; nothing below can be about it")
			}
			if len(want) > trace.MaxTraceState {
				t.Fatalf("the door handed out %d bytes, more than its own bound of %d", len(want), trace.MaxTraceState)
			}
			// Every entry that survived is a whole entry of the caller's, never
			// a slice of one — the rule 3809d04 states and W3C asks for.
			for _, entry := range strings.Split(want, ", ") {
				if !strings.Contains(tc.state, entry) {
					t.Errorf("kept entry %q is not a whole entry of the caller's header", entry)
				}
			}

			tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
			name := "ledger_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "") + "." + tc.suffix
			req := tenancy.WithTenant(t.Context(), tenant)
			if err := db.Run(trace.With(req, in), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return Publish(ctx, tx, name, map[string]bool{"traced": true})
			}); err != nil {
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
			var doc map[string]any
			if err := json.Unmarshal(msg.Data, &doc); err != nil {
				t.Fatalf("the relayed body is not JSON: %v (%s)", err, msg.Data)
			}

			// 1. The envelope names the trace, and the vendor's memory of it.
			if doc["traceparent"] != parent {
				t.Errorf("traceparent = %v, want the caller's %q", doc["traceparent"], parent)
			}
			state, ok := doc["tracestate"].(string)
			if !ok {
				t.Fatalf("the envelope of an event a traced request caused carries no tracestate string: %s", msg.Data)
			}
			if state != want {
				t.Errorf("envelope tracestate = %q, want the value the door handed out %q", state, want)
			}
			if len(state) > trace.MaxTraceState {
				t.Errorf("envelope tracestate is %d bytes, more than the kernel's own bound of %d", len(state), trace.MaxTraceState)
			}

			// 2. The row holds one and the same string, not a second copy of
			// the caller's header.
			var column *string
			if err := admin.QueryRowContext(t.Context(),
				`SELECT tracestate FROM platformkit_outbox WHERE name=$1`, name).Scan(&column); err != nil {
				t.Fatal(err)
			}
			if column == nil {
				t.Fatalf("the outbox row of an event caused by a traced request stores no tracestate")
			}
			if *column != state {
				t.Errorf("the row carries %q and the envelope %q; 3809d04 promises one bounded value, not two", *column, state)
			}

			// 3. A subscriber outside the process gets the same value back.
			var ev Event
			if err := json.Unmarshal(msg.Data, &ev); err != nil {
				t.Fatalf("the envelope this relay published does not read back: %v", err)
			}
			if ev.TraceState != state {
				t.Errorf("delivery tracestate = %q, want %q", ev.TraceState, state)
			}
		})
	}
}
