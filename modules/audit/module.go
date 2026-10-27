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
	"github.com/septagon-oss/platformkit/kit/personaldata"
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
	// somebody else's. config.Audit is where it comes from, and config.Validate is
	// what refuses a period below the floor migrations/000048 spells.
	RetentionDays int

	// RetainURL is the role that may expire the trail: database.retain_url, a
	// connection that may DELETE audit_events and may not INSERT them, which is the
	// only shape the expiry trigger admits. Empty means this installation never
	// expires history, and the job says so instead of deleting nothing quietly. The
	// module names no role and its SQL never names one either — module SQL may name
	// no role — so the composition hands over the DSN and grants it.
	RetainURL string

	// Feature is the plan feature the trail belongs to, or empty when this
	// installation gives it to everybody. It is a dependency and not a constant
	// for the same reason RetentionDays is: which features a product sells is
	// the composition's decision, and a module that chose one would be pricing
	// somebody else's product. See httpx.Auth.Needing.
	Feature string
}

// retentionDays is the default, and config.DefaultRetentionDays is the same
// number for the same reason; this one is here so that a zero Deps is still a
// working module.
const retentionDays = 365

var permissions = []module.Permission{{Key: contracts.PermissionAuditRead, Label: "read the audit trail"}}

// Module is the manifest, and the service it is built on.
//
// Deprecated: use NewChecked, which answers this refusal as an error.
func New(deps Deps) module.Module {
	m, err := NewChecked(deps)
	if err != nil {
		panic(err.Error())
	}
	return m
}

// NewChecked builds the manifest and the retention job. A RetentionDays below
// one keeps the default year and an empty RetainURL means this installation
// never expires history, so nothing is refused and the error is nil for every
// Deps today. It opens nothing, runs no query and is safe to call twice.
func NewChecked(deps Deps) (module.Module, error) {
	svc := internal.NewService()
	days := deps.RetentionDays
	if days <= 0 {
		days = retentionDays
	}
	return module.Module{
		Name:       "audit",
		Migrations: Migrations.Files,
		Adopts:     Migrations.Adopts,
		RulesFrom:  Migrations.RulesFrom,
		// The trail names a person by `actor` and repeats whatever the event carried
		// inside `payload` — an address verbatim today — so the payload is filed as
		// text an erasure has to scrub and not as a column that names nobody.
		PersonalData: personaldata.Implements(personaldata.Table{
			Name:   "audit_events",
			Person: "actor",
			// The person a run served without signing in, and the person the
			// admitted operation names as its own: a uuid that names a person the
			// trail is about, and an erasure that left it behind would keep pointing
			// at the person it says it forgot.
			About: []string{"initiator", "operation_person_id"},
			Text:  []string{"payload"},
			// The operation record and the tenant it ran under: rows, not people.
			NotPerson: []string{"event_id", "records", "operation_ref", "operation_tenant_id"},
		}),
		Permissions: permissions,
		// None, and the absence is the decision: see the package comment.
		Declared: nil,
		Nav: []module.NavEntry{
			{Label: "Audit", Screen: "audit/events", Permission: contracts.PermissionAuditRead},
		},
		Jobs: []jobs.Job{internal.Retention(deps.Tenants, days, deps.RetainURL)},
		// One handler, and the kernel writes the names: SubscribeAll is
		// expanded into one subscription per declared event after every
		// manifest has been read, so a module added to the composition after
		// this one is audited by having emitted an event and by nothing else.
		SubscribeAll:  true,
		Subscriptions: []events.Subscription{{Module: "audit", Handler: svc.Record}},
		Routes:        func(s httpx.Surfaces) { internal.RegisterRoutes(s.App, svc, deps.Feature) },
	}, nil
}
