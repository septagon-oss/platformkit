// Package webhook is the module manifest: what the kernel needs to know about
// outbound webhooks that it cannot learn from a function call.
//
// A webhook endpoint is an HTTPS address a tenant registered so that a machine
// of its own hears what happened here. The platform already had the hard half:
// every event leaves through the transactional outbox and is described in the
// composition's AsyncAPI document. What was missing was a way for a customer to
// subscribe from outside, and a signature a receiver could check.
//
// Delivery consumes the outbox stream the way the audit trail does — one
// SubscribeAll subscription the kernel expands per emitted event, so a module
// composed after this one is delivered by having emitted an event — and the
// handler writes rows rather than opening sockets. A second job is what sends:
// it claims rows in a short tenant transaction, commits, then talks HTTP with no
// lock held, and records the outcome in a transaction of its own. The retry
// ladder, the three-day window, and the disablement that follows them live in
// those rows and this module's constants, not in the broker's five rungs.
//
// Signing follows Standard Webhooks: webhook-id, webhook-timestamp and
// webhook-signature (v1, HMAC-SHA256) over the stored CloudEvent bytes, with a
// whsec_ secret shown once at the create and the rotate and readable by nothing
// afterwards. README.md carries the receiver's check in twelve lines of Go.
package webhook

import (
	"context"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
	"github.com/septagon-oss/platformkit/modules/webhook/internal"
)

// Deps is what this module cannot make for itself: a struct rather than a
// parameter list, so adding one is a named field at every call site.
type Deps struct {
	// SecretKey is the 32 bytes the signing secrets are wrapped with —
	// deployment configuration read once at composition, which no tenant can
	// read or set. A deployment that never composes an endpoint needs none, and
	// every deployment that does must have one: see the module README.
	SecretKey []byte

	// Catalogue is what this composition can emit. Required: every write checks
	// an endpoint's event list against it, and no module that is not built last
	// can enumerate it. The composition writes the implementation as a literal
	// beside the literal its message catalogues already are.
	Catalogue contracts.Catalogue

	// Alerts optionally tells somebody when an endpoint stopped. nil is legal
	// and means the disablement happens, publishes and is logged with nobody
	// notified.
	Alerts contracts.Alerts

	// Tenants is how the delivery job and the retention sweep reach every
	// tenant, answered by modules/tenant the way modules/task and modules/audit
	// take it.
	Tenants jobs.TenantLister

	// AllowLocalEndpoints permits loopback and private ranges to be dialled. It
	// is deployment-level: there is no column, field or route that can carry it,
	// and it never reaches the instance-metadata address. It exists for a
	// developer's laptop and this module's own tests.
	AllowLocalEndpoints bool

	// RetentionDays is how long a finished attempt is kept; zero means the
	// default below. Every delivery row keeps the whole event it signed, so
	// forgetting is the other half of storing it.
	RetentionDays int

	// Every is how often the delivery job runs; zero means five seconds, which
	// is the lag an event has before it leaves. A test sets it lower.
	Every time.Duration

	// Now is the clock the sender computes with: the ladder's arithmetic and the
	// webhook-timestamp header. nil means the wall clock. Row timestamps are the
	// database's.
	Now func() time.Time
}

// retentionDays is the default, and a zero Deps still gets one: a module that
// kept every signed payload forever would be a module with an outage dated on it.
const retentionDays = 90

// permissions is what the manifest declares. kit/app checks every route's
// declaration against it at boot, so a route guarded by a permission that is not
// here fails startup instead of denying everyone forever.
var permissions = []module.Permission{
	{Key: contracts.PermissionWebhookRead, Label: "read webhook endpoints and their deliveries"},
	{Key: contracts.PermissionWebhookUpdate, Label: "register, change, enable and disable webhook endpoints"},
}

// endpointSpec is the resource's presence in the application. It offers list,
// read, update and delete: not create, because the create's answer is the
// signing secret and a Spec answers with the row.
//
// The Immutable list is the design, not a detail. url and events are the two
// fields that decide *where events go*, and POST /endpoints/{id}/target
// re-checks both the way the create does; status and the three fields beside it
// are the lifecycle a command owns; createdBy is the actor the create stamped.
// What is left for the generic PATCH is the name — a label, with no rule
// attached to it.
var endpointSpec = rest.Spec[*contracts.Endpoint]{
	Module:     "webhook",
	Entity:     "endpoint",
	Path:       "/endpoints",
	Read:       contracts.PermissionWebhookRead,
	Write:      contracts.PermissionWebhookUpdate,
	Operations: []httpx.CRUD{rest.List, rest.Read, rest.Update, rest.Delete},
	SoftDelete: true,
	Immutable: []string{"url", "events", "status", "disabledAt", "disabledReason",
		"lastSuccessAt", "createdBy"},
	// The Spec publishes the update and the delete; the create and the two
	// lifecycle commands name theirs where they are mounted. All five are in
	// contracts.Events, and kit/app refuses the difference.
	HookEvents: []string{contracts.EventEndpointUpdated, contracts.EventEndpointDeleted},
}

// deliverySpec is the log's read side: two routes, both guarded by read, and no
// write at all. A verb left out mounts no route, no door and no page, which is
// how this module says a delivery row is a fact rather than a resource: nobody
// creates one with a PATCH, and the one write that exists — a replay — is a
// command on the log that answers with a *new* row.
var deliverySpec = rest.Spec[*contracts.Delivery]{
	Module:     "webhook",
	Entity:     "delivery",
	Path:       "/deliveries",
	Read:       contracts.PermissionWebhookRead,
	Write:      contracts.PermissionWebhookUpdate,
	Operations: []httpx.CRUD{rest.List, rest.Read},
}

// New is the manifest. It panics on a missing key or catalogue, at the
// composition site: both are read by every write, and a module that started
// anyway would be a module whose first create failed at runtime.
func New(deps Deps) module.Module {
	svc := internal.NewService(internal.Deps{
		SecretKey: deps.SecretKey,
		Catalogue: deps.Catalogue,
		Alerts:    deps.Alerts,
		Policy:    internal.AddressPolicy{AllowLocal: deps.AllowLocalEndpoints},
		Now:       deps.Now,
	})
	days := deps.RetentionDays
	if days <= 0 {
		days = retentionDays
	}
	// The one thing the endpoint Spec cannot say on its own: deleting an endpoint
	// ends its pending attempts with a reason in the same transaction, so the
	// drain never POSTs to an address a caller stopped trusting, and the history
	// of what was already delivered stays listable.
	mounted := endpointSpec
	mounted.AfterDelete = func(_ context.Context, tx db.Tx[db.Tenant], e *contracts.Endpoint) error {
		return svc.DropQueued(tx, e.ID, contracts.ReasonEndpointDeleted)
	}
	return module.Module{
		Name:        "webhook",
		Migrations:  Migrations.Files,
		Permissions: permissions,
		Declared:    contracts.Events,
		Nav: []module.NavEntry{
			{Label: "Endpoints", Screen: "webhook/endpoints", Permission: contracts.PermissionWebhookRead},
			{Label: "Deliveries", Screen: "webhook/deliveries", Permission: contracts.PermissionWebhookRead},
		},
		Jobs: []jobs.Job{
			internal.Deliver(svc, deps.Tenants, deps.Every, 0, 0, 0),
			internal.Retention(deps.Tenants, days),
		},
		// One handler, and the kernel writes the names: the same mechanism
		// modules/audit uses, so this module hears every event of its own app and
		// no other app's, once each.
		SubscribeAll:  true,
		Subscriptions: []events.Subscription{{Module: "webhook", Handler: svc.Fanout}},
		Routes: func(s httpx.Surfaces) {
			mounted.Mount(s)
			deliverySpec.Mount(s)
			internal.RegisterRoutes(s, mounted, svc)
		},
	}
}
