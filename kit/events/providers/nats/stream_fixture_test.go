package nats_test

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/septagon-oss/platformkit/kit/appname"
)

// TestMain owns the address space the app-scoped cases in this package publish
// into, which is the one thing about this package's fixture that is not visible
// from any single case.
//
// Every case here subscribes the way the shipped transport does — a durable consumer
// on the single PLATFORMKIT stream, asking for DeliverAll, because that is what
// kit/events/providers/nats asks for (jetstream.go, and the reconcile that refuses
// to let a live consumer hold a different deliver policy). DeliverAll means what it
// says: a consumer made now is owed this app's whole retained history, a week of it
// (delivery.Keep). For the kernel that is the point — platformkit_handled claims each
// event before its handler runs, which is the reply to a replay, and jetstream.go
// says so at the place it asks for the policy. A raw counting handler in a test has
// no ledger to answer the replay with, so a message an earlier run published at the
// same address is counted as a delivery of this run's event.
//
// That is not hypothetical here: measured on this task's broker, with 284 messages
// sitting at app acme's own address from earlier runs of these cases, the two-apps
// case reported "an event published at platformkit.acme-billing… reached app acme's
// handler" — a cross-app delivery that had never happened, read off a backlog of
// that app's own traffic. So the fixture clears the apps' spaces before any case
// runs. It deletes nothing outside the app spaces these cases name, and it is run
// against the test broker the Makefile names, the one `make down` declares disposable.
func TestMain(m *testing.M) {
	url := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if url == "" {
		// Every case in this package fails with its own sentence when the broker
		// is unset; the fixture has nothing to clear and says nothing about it.
		os.Exit(m.Run())
	}
	nc, err := nats.Connect(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nats fixture: connect to the test broker: %v\n", err)
		os.Exit(1)
	}
	defer nc.Close()
	js, err := nc.JetStream()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nats fixture: open JetStream: %v\n", err)
		os.Exit(1)
	}
	for _, app := range fixtureApps {
		// Space is the app's own token with its separator: the same constructor the
		// kernel's AsyncAPI document spells its addresses with, so the fixture
		// cannot widen an app's space by accident in the way a hand-written prefix
		// could.
		if err := js.PurgeStream(sharedStream, &nats.StreamPurgeRequest{Subject: appname.Space(app) + ">"}); err != nil &&
			!errors.Is(err, nats.ErrStreamNotFound) {
			fmt.Fprintf(os.Stderr, "nats fixture: clear app %s's address space: %v\n", app, err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

// fixtureApps are the app slugs the cases in this package publish as: the pair
// whose durable names collide if the app segment is not separable.
var fixtureApps = []appname.Name{
	appname.MustParse("acme"),
	appname.MustParse("acme-billing"),
}
