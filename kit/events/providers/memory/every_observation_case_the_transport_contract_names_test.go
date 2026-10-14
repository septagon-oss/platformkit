package memory_test

import (
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/events/transport/observetest"
)

// TestObservationConformance runs the transport's own observation cases against
// the in-process provider. The app slug is ignored: this transport routes inside
// one process by tenant and event name and writes no address, which is also why
// it has no address mismatch to check and nothing to forge one with.
//
// Quiet is a few milliseconds because a notice here is not a network trip: the
// in-process transport hands every observation on the publisher's goroutine, so
// once Publish has returned, everything this process could deliver has arrived.
func TestObservationConformance(t *testing.T) {
	observetest.Run(t, observetest.Deps{
		Name: "memory",
		Transport: func(_ *testing.T, _ appname.Name) (transport.Transport, func()) {
			return memory.New(), func() {}
		},
		// One value, twice: an in-process transport has no connection to make twice,
		// because the process it lives in is the whole broker. Two separate values
		// would be two brokers that never spoke, and the case would be checking that
		// traffic never crosses between compositions that were never shared — which
		// another case in this package already checks about subscriptions.
		Processes: func(_ *testing.T, _ appname.Name) (transport.Transport, transport.Transport, func()) {
			bus := memory.New()
			return bus, bus, func() {}
		},
		Quiet: func() time.Duration { return 5 * time.Millisecond },
		// The slug is stamped into the event's own claim and nothing else: this
		// transport routes by tenant and event name inside one process, so it writes
		// no address that could carry an app and has no address space to clear.
		App: appname.MustParse("inprocess"),
	})
}
