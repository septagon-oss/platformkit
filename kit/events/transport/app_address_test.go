// An address carries the app for one reason: so that the app reading a message
// can refuse one that is not its own. These cases hold that comparison for a
// composition that names its app — the one decision 0074 puts two of on a broker
// — where the tenant-only address this kernel checked every delivery against
// before the app token existed says nothing about which app a message belongs to.
//
// The two addresses the rollout window filters are the point of the file. A
// subscription of an app-scoped build filters them on purpose, so that a
// publisher still running a previous build is read while it is alive; what stops
// another app's previous build being read with it is not the filter, which cannot
// tell the two apart, but this comparison. A delivery that arrives at an address
// naming no app cannot be shown to be this app's, and is refused: the boundary,
// and not the wildcard, is what decides.
package transport_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

func TestAnAppScopedDeliveryArrivesOnlyAtItsOwnAppAddress(t *testing.T) {
	tenant := uuid.New()
	const name = "billing.plan.created"
	acme := appname.MustParse("acme")
	// The document carries the app its publisher stamped into it — what
	// providers/nats does on the way out (its Publish sets Event.App to the app the
	// transport was made with), so a message at a scoped address with no app inside
	// it is not this kernel's delivery and transport.AddressMismatch says so. The
	// comparison below is of a delivery this build could actually produce.
	ev := transport.Event{ID: uuid.New(), Name: name, TenantID: tenant, App: acme}

	// The address this build publishes at, for the app that published it. The
	// transport publishes here (providers/nats publishes at AppSubject) and
	// subscribes with this app's filter, so a delivery that arrived anywhere
	// else is the one case the comparison exists for.
	if err := transport.AddressMismatch(acme, transport.AppSubject(acme, tenant, name), ev); err != nil {
		t.Errorf("app acme's own address refused its own event: %v", err)
	}

	for _, bad := range []struct{ subject, claim string }{
		{transport.AppSubject(appname.MustParse("acme-billing"), tenant, name),
			"a second app's address, whose tenant token is the same uuid because nothing about a tenant id says which app holds it"},
		{transport.Subject(tenant, name),
			"the previous build's address, which names no app and so cannot be shown to be this app's"},
		{appname.OldestSubject(name),
			"the address before the tenant token, which names neither app nor tenant"},
	} {
		if err := transport.AddressMismatch(acme, bad.subject, ev); err == nil {
			t.Errorf("an app-scoped delivery at %q was accepted: %s", bad.subject, bad.claim)
		} else if !strings.Contains(err.Error(), bad.subject) {
			t.Errorf("the refusal %q does not name the address %q it refused", err, bad.subject)
		}
	}
}

// TestTheWideFilterOfAnAppScopedSubscriptionIsAnsweredByTheCheckAndNotByTheFilter
// walks all three addresses an app-scoped consumer filters and asks, of each,
// whether a delivery that arrived there may run. Exactly one may. The other two
// stay in the filter for the length of a rollout, and what makes reading them
// safe is this comparison refusing them, which is the sentence
// appname.PreviousFilter points at and the one the app check had to exist for.
func TestTheWideFilterOfAnAppScopedSubscriptionIsAnsweredByTheCheckAndNotByTheFilter(t *testing.T) {
	tenant := uuid.New()
	const name = "task.task.created"
	acme := appname.MustParse("acme")
	// Stamped the way the publisher stamps it: the walk is over addresses, and the
	// document has to be the one that travels at the address under test.
	ev := transport.Event{ID: uuid.New(), Name: name, TenantID: tenant, App: acme}

	accepted := 0
	for _, filter := range transport.AppFilters(acme, name) {
		arrived := filter
		if strings.Contains(filter, "*") {
			arrived = strings.Replace(filter, "*", tenant.String(), 1)
		}
		if err := transport.AddressMismatch(acme, arrived, ev); err != nil {
			continue
		}
		accepted++
		if arrived != transport.AppSubject(acme, tenant, name) {
			t.Errorf("a delivery that arrived at %q was accepted by app acme, which publishes only at %q",
				arrived, transport.AppSubject(acme, tenant, name))
		}
	}
	if accepted != 1 {
		t.Errorf("of the %d addresses app acme's consumer filters, %d may run a delivery; the two rollout addresses must be read but never answered",
			len(transport.AppFilters(acme, name)), accepted)
	}
}

// TestADeploymentWithNoSlugAnswersNoAppScopedAddress is the same rule from the
// other side. A composition that sets no slug hosts one app and has no app token
// to agree with, so scoped traffic is another app's by definition, and the two
// unscoped addresses stay readable exactly as they were before decision 0074.
func TestADeploymentWithNoSlugAnswersNoAppScopedAddress(t *testing.T) {
	tenant := uuid.New()
	const name = "notes.created"
	ev := transport.Event{ID: uuid.New(), Name: name, TenantID: tenant}

	for _, want := range []struct{ subject, claim string }{
		{transport.Subject(tenant, name), "the address this deployment publishes at"},
		{appname.OldestSubject(name), "the previous build's address"},
	} {
		if err := transport.AddressMismatch(oneApp, want.subject, ev); err != nil {
			t.Errorf("the unscoped deployment refused %q, %s: %v", want.subject, want.claim, err)
		}
	}
	if err := transport.AddressMismatch(oneApp, transport.AppSubject(appname.MustParse("acme"), tenant, name), ev); err == nil {
		t.Error("the unscoped deployment accepted an address naming an app it is not")
	}
}

// TestAnAppNameThatIsNotASlugAnswersNoAddress is the same refusal the address
// constructors make (appname.token yields the empty segment for a Name built by
// conversion, which would collapse two tokens of an address into one) carried
// through to the boundary: a comparison built on a broken name matches no
// address, so nothing is delivered rather than something delivered to everyone.
func TestAnAppNameThatIsNotASlugAnswersNoAddress(t *testing.T) {
	tenant := uuid.New()
	const name = "notes.created"
	broken := appname.Name("Acme Corp")
	ev := transport.Event{ID: uuid.New(), Name: name, TenantID: tenant}

	for _, subject := range []string{
		appname.Subject(broken, tenant, name),
		transport.Subject(tenant, name),
		appname.OldestSubject(name),
		"",
	} {
		if err := transport.AddressMismatch(broken, subject, ev); err == nil {
			t.Errorf("a delivery at %q was accepted on the broken app name %q", subject, broken)
		}
	}
}
