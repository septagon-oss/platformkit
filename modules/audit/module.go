// Package audit is the module manifest: what the kernel needs to know about the
// audit trail that it cannot learn from a function call.
//
// It is composed last and takes the whole composition as an argument, because
// what it does is subscribe to everything: main computes the union of every
// other module's declared events with module.EventNames and hands it over. A
// module that emits an event is audited by having emitted it, and nothing is
// registered anywhere to make that true.
//
// It declares no events of its own. An audit of audits is a loop.
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
	// Events is every event the trail records: the union of what every other
	// module declares, which main computes with module.EventNames after the
	// others are constructed. A hand-written list here would be one somebody
	// has to remember to extend, and the failure of that is silence.
	Events []string

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
	// One subscription per event name rather than one wildcard: the kernel's
	// durable consumers are named after the subscription, so a wildcard would
	// be a single consumer whose backlog is every event in the system and one
	// slow payload would hold up the trail of everything else.
	subs := make([]events.Subscription, 0, len(deps.Events))
	for _, name := range deps.Events {
		subs = append(subs, events.Subscription{Module: "audit", Name: name, Handler: svc.Record})
	}
	return module.Module{
		Name:        "audit",
		Permissions: permissions,
		// None, and the absence is the decision: see the package comment.
		Events: nil,
		Nav: []module.NavEntry{
			{Label: "Audit", Path: "/admin/audit/events", Permission: contracts.PermissionAuditRead},
		},
		Jobs:          []jobs.Job{internal.Retention(deps.Tenants, days)},
		Subscriptions: subs,
		Routes:        func(api *httpx.API) { internal.RegisterRoutes(api, svc) },
	}
}
