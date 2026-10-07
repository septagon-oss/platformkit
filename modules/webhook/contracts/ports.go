package contracts

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
)

// Catalogue answers the one question the module cannot ask itself: which event
// types does *this* composition emit? An endpoint's list is only checkable
// against what the application can actually deliver, and no module that is not
// built last can enumerate that — the composition knows it, so the composition
// writes the answer.
//
// The reference application writes it as a literal assembled from the other
// modules' own contracts constants, beside the literal its message catalogues
// already are, and one test in that app asserts the literal's set equals the set
// its composition emits. That is the drift risk closed with a failing test
// rather than an argument.
//
// Delivery never consults it. A subscription whose event type disappears from a
// later composition is therefore not a refusal at boot and not a broken
// delivery: it is a row that simply never matches, and the endpoint's delivery
// log says so by staying empty for that name. A stale catalogue can offer an
// option that never fires; it cannot invent a delivery.
type Catalogue interface {
	// EventTypes returns every event name this composition can emit, the names
	// a caller may list on an endpoint.
	EventTypes() []string
}

// Alerts tells somebody when an endpoint stopped working. The composition
// implements it over its notification service and decides the channel, the
// words and the recipient template; this module names only the fact and the
// endpoint it happened to, and hands back the row so the implementation can
// address the person who registered it.
//
// It runs inside the outcome transaction, so the notice and the disablement and
// the outbox event are one commit: a disabled endpoint nobody was told about is
// the failure mode this port exists to remove.
//
// nil is a legal composition: the disablement still happens, still publishes,
// still writes the log, and notifies nobody. A deployment that tells people some
// other way is not a deployment that loses the fault rule.
type Alerts interface {
	EndpointDisabled(ctx context.Context, tx db.Tx[db.Tenant], e *Endpoint) error
}
