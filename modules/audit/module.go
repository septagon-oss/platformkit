// Package audit is the module manifest: what the kernel needs to know about the
// audit trail that it cannot learn from a function call.
//
// What it does is subscribe to everything, and the manifest says so with one
// field: module.SubscribeAll. The kernel expands it into one subscription per
// declared event once every manifest is in hand, so a module that emits an
// event is audited by having emitted it — wherever it sits in the composition,
// and with nothing registered anywhere to make that true.
//
// main used to compute the list and pass it in, which worked only while audit
// was composed last. A module listed after it was a module nothing recorded,
// and the failure was silence.
//
// It declares no events of its own. An audit of audits is a loop.
//
// # What a payload may carry
//
// The trail stores every payload as its module published it, verbatim, and that
// is the design: what happened is the event's name and the module's own
// account of it, and a schema that tried to normalise those would be a schema
// every new module had to be taught. The price is a convention, and it is
// stated here because this is the module that pays it — an event carries
// identifiers, not content.
//
// It is a convention rather than a check, because nothing can read a payload
// and tell a title from a secret. What it is now is a convention with one
// enforced case: notification.email_requested used to carry the whole message,
// address and body and link, which put every notice in the audit trail and
// would have put every set-password link there the day one was mailed. It
// carries two identifiers now, and the worker reads the row back. A payload
// that carries content is a payload kept for a year in a table an
// administrator can read.
package audit

import (
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

// Deps is what this module cannot make for itself.
type Deps struct {
	// Tenants is how the retention sweep reaches every tenant.
	Tenants jobs.TenantLister

	// RetentionDays is how long a row is kept; zero means a year. It is a
	// dependency rather than a constant because a retention period is a
	// compliance obligation, and a module that chose one would be choosing
	// somebody else's. config.Audit is where it comes from.
	RetentionDays int
}

// retentionDays is the default, and config.DefaultRetentionDays is the same
// number for the same reason; this one is here so that a zero Deps is still a
// working module.
const retentionDays = 365

var permissions = []module.Permission{{Key: contracts.PermissionAuditRead}}

// Module is the manifest, and the service it is built on.
func Module(deps Deps) module.Module {
	svc := internal.NewService()
	days := deps.RetentionDays
	if days <= 0 {
		days = retentionDays
	}
	return module.Module{
		Name:        "audit",
		Permissions: permissions,
		// None, and the absence is the decision: see the package comment.
		Events: nil,
		Nav: []module.NavEntry{
			{Label: "Audit", Path: "/admin/audit/events", Permission: contracts.PermissionAuditRead},
		},
		Jobs: []jobs.Job{internal.Retention(deps.Tenants, days)},
		// One handler, and the kernel writes the names: SubscribeAll is
		// expanded into one subscription per declared event after every
		// manifest has been read, so a module added to the composition after
		// this one is audited by having emitted an event and by nothing else.
		SubscribeAll:  true,
		Subscriptions: []events.Subscription{{Module: "audit", Handler: svc.Record}},
		Routes:        func(api *httpx.API) { internal.RegisterRoutes(api, svc) },
	}
}
