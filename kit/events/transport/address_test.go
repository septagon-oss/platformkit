// The address a message arrived at is a fact the broker knows and the document
// does not get to decide alone: a consumer's filter fixes an event's name and
// leaves its tenant a wildcard (Filters), and events.Consume opens the handler's
// transaction in the tenant the document names. These cases hold the comparison
// that keeps those two facts from being one tenant's rows reached from another's
// message. The end-to-end version — a forged pair put on a live broker and the
// handler's own read of row-level security as the witness — is
// kit/events' TestAMessageStoredOnOneTenantsAddressIsNotDeliveredInsideAnotherTenantsTransaction;
// what belongs here is the rule itself, including the two addresses that pass.
package transport_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events/transport"
)

func TestADeliveryIsCheckedAgainstTheAddressItArrivedAt(t *testing.T) {
	tenant := uuid.New()
	other := uuid.New()
	const name = "notes.created"
	ev := transport.Event{ID: uuid.New(), Name: name, TenantID: tenant,
		Payload: json.RawMessage(`{"id":"n-1"}`)}

	for _, want := range []struct {
		subject string
		claim   string
	}{
		{transport.Subject(tenant, name), "the address this build publishes at"},
		{"platformkit." + name, "the previous build's address, which names no tenant"},
	} {
		if err := transport.AddressMismatch(want.subject, ev); err != nil {
			t.Errorf("AddressMismatch(%q, %s in tenant %s) = %v, want nil: %s",
				want.subject, name, tenant, err, want.claim)
		}
	}

	for _, bad := range []struct {
		subject, claim string
	}{
		{transport.Subject(other, name), "a message stored on another tenant's address and stamped as this tenant's"},
		{transport.Subject(tenant, "notes.deleted"), "a body whose event was rewritten while its address stayed"},
		{transport.Subject(other, "notes.deleted"), "a body whose tenant and event were both rewritten"},
		{"platformkit." + other.String() + ".notes", "an address with one token missing, which no filter of this subscription answers"},
		{"platformkit.>.notes.created", "a wildcard written into an address rather than a filter"},
		{"acme." + other.String() + "." + name, "another program's namespace, which is not this event's address either"},
		{"", "no address at all"},
	} {
		err := transport.AddressMismatch(bad.subject, ev)
		if err == nil {
			t.Errorf("AddressMismatch(%q, %s in tenant %s) = nil, want a refusal: %s",
				bad.subject, name, tenant, bad.claim)
			continue
		}
		// The refusal has to name the address it refused, or an operator
		// reading a broker's log line cannot tell which of the two copies moved.
		if !strings.Contains(err.Error(), bad.subject) {
			t.Errorf("the refusal %q does not name the address %q it refused", err, bad.subject)
		}
	}
}

// TestEveryAddressASubscriptionAnswersIsOneADeliveryMayArriveAt walks the
// addresses a consumer filters and the tenants that can fill them: the tenant
// token is a wildcard, so every tenant's own address has to pass the check for
// the event that names it, and the window's second address — the one with no
// tenant in it at all — passes however the document is stamped. If Filters ever
// grows a third address, this is the test that asks whether the check knows it.
func TestEveryAddressASubscriptionAnswersIsOneADeliveryMayArriveAt(t *testing.T) {
	tenants := []uuid.UUID{uuid.New(), uuid.New()}
	for _, name := range []string{"notes.created", "billing.invoice_voided.posted"} {
		for _, filter := range transport.Filters(name) {
			if strings.Contains(filter, "*") {
				if got := strings.Count(filter, "."); got != strings.Count(name, ".")+2 {
					t.Fatalf("filter %q does not have the one wildcard token the tenant segment is", filter)
				}
				for _, tenantID := range tenants {
					arrived := strings.Replace(filter, "*", tenantID.String(), 1)
					ev := transport.Event{ID: uuid.New(), Name: name, TenantID: tenantID}
					if err := transport.AddressMismatch(arrived, ev); err != nil {
						t.Errorf("a message that arrived at %q, matched by the filter %q, was refused: %v", arrived, filter, err)
					}
				}
				continue
			}
			// The address with no tenant in it: any tenant's event may arrive there.
			for _, tenantID := range tenants {
				ev := transport.Event{ID: uuid.New(), Name: name, TenantID: tenantID}
				if err := transport.AddressMismatch(filter, ev); err != nil {
					t.Errorf("the previous build's address %q refused %s in tenant %s: %v", filter, name, tenantID, err)
				}
			}
		}
	}
}
