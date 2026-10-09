package appname_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

// TestADurableNameBelongsToExactlyOneApp pins the claim the durable constructor
// exists for: the name says which app's subscription owns the consumer.
//
// The durable is not a label an app keeps to itself. It is the JetStream consumer
// name on the one PLATFORMKIT stream every app shares
// (kit/events/providers/nats/jetstream.go's stream constant), it is the queue
// group that name joins (group(durable) there), and it is half of the primary key
// of platformkit_handled and platformkit_dead_letters
// (migrations/000003_handled.up.sql:23, 000005_dead_letters.up.sql:24), so it is
// also what kit/events/replay.go selects a dead letter by. Two subscriptions
// answering to one durable name are therefore one consumer, one queue group and
// one handled claim: whichever process binds second either fails to bind at all
// or shares the first's state.
//
// The app slug is joined to the module and the event with a dash, and a dash is
// legal inside a slug (Parse admits "acme-billing"), so the join has to keep the
// three parts separable. Two apps that a deployment could plausibly host together
// — one client "acme" and one client "acme-billing" — name two of this
// repository's own conventions, and the names the constructors answer to are
// formed by different authors on each side.
func TestADurableNameBelongsToExactlyOneApp(t *testing.T) {
	acme := appname.MustParse("acme")
	acmeBilling := appname.MustParse("acme-billing")

	// Both event names clear the one grammar every event name passes: kit/module
	// checks a manifest's Events with transport.ValidName, so a name that boots
	// is a name whose durable is formed. "billing.plan.created" is this
	// repository's own naming convention (modules/billing/contracts/events.go).
	for _, name := range []string{"billing.plan.created", "plan.created"} {
		if !transport.ValidName(name) {
			t.Fatalf("the case's event name %q is not one the kernel would publish", name)
		}
	}

	one := appname.Durable(acme, "billing", "billing.plan.created")
	other := appname.Durable(acmeBilling, "billing", "plan.created")
	if one == other {
		t.Errorf("acme/billing/billing.plan.created and acme-billing/billing/plan.created form the same durable %q: two apps share one consumer, one queue group and one handled-ledger key", one)
	}

	// The same failure with the module absorbed rather than the event: any app
	// slug that ends in another app's module name re-splits the address.
	cases := []struct{ app1, mod1, ev1, app2, mod2, ev2 string }{
		{"acme", "billing", "billing.plan.created", "acme-billing", "billing", "plan.created"},
		{"acme", "content", "content.content.created", "acme-content", "content", "content.created"},
		{"acme", "task", "task.task.updated", "acme-task", "task", "task.updated"},
	}
	for _, c := range cases {
		a := appname.Durable(appname.MustParse(c.app1), c.mod1, c.ev1)
		b := appname.Durable(appname.MustParse(c.app2), c.mod2, c.ev2)
		if a == b {
			t.Errorf("%s/%s/%s and %s/%s/%s form the same durable %q", c.app1, c.mod1, c.ev1, c.app2, c.mod2, c.ev2, a)
		}
	}
}
