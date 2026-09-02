// Package billing is the module manifest: what a tenant is paying for.
//
// It is the shape every module follows — contracts/, internal/, and one
// exported function taking a struct of typed dependencies and returning a
// module.Module — with one asymmetry worth naming at the top. Plans are a
// collection an administrator maintains, so they are a rest.Spec. The
// subscription is not a collection: a tenant is the customer, so it has one,
// and a singleton has no list, no create and no id in its path. Its three
// routes are internal/handler.go, the only hand-written ones here.
//
// Renewal is the other thing to read first. Taking money is a call to somebody
// else's machine, so it never happens inside a database transaction: the job
// asks the service what is owed, closes that transaction, charges, and opens
// another to record the answer. internal/renew.go is the shape.
package billing

import (
	"time"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/billing/contracts"
	"github.com/septagon-oss/platformkit/modules/billing/internal"
)

// Manual is the PaymentProvider that records what is owed and moves no money.
// main wires it, or whatever a deployment has instead, so the choice is visible
// in the file that composes the application — as the mailer is.
var Manual = internal.NewManual

// Deps is what this module cannot make for itself.
type Deps struct {
	// Tenants is how the nightly renewal reaches every tenant.
	Tenants jobs.TenantLister

	// Payments takes the money. There is one implementation here, Manual; the
	// ones that speak to a payment processor live outside this repository.
	Payments contracts.PaymentProvider

	// RenewEvery replaces the nightly schedule with an interval, for a test
	// that cannot wait until two in the morning. Zero means the schedule.
	RenewEvery time.Duration
}

// spec is the plan catalogue's presence in the application: five routes, two
// permissions, three events and the schema a generated screen reads.
//
// It names no Immutable, and the absence is a decision: a plan is data an
// administrator maintains, and changing a field changes what the next renewal
// charges and nothing else, because a subscription's period is its own. The
// fields a command owns are all on the subscription, which is not a Spec. The
// one hook it does have, on delete, declares no HookEvents because all it ever
// does is refuse.
var spec = rest.Spec[*contracts.Plan]{
	Module:     "billing",
	Entity:     "plan",
	Path:       "/api/v1/billing/plans",
	Read:       contracts.PermissionBillingRead,
	Write:      contracts.PermissionBillingManage,
	SoftDelete: true,
}

// permissions is what the manifest declares. kit/app checks every route's
// declaration against it at boot, so a route guarded by a permission that is
// not here fails startup instead of denying everyone forever.
var permissions = []module.Permission{
	{Key: contracts.PermissionBillingRead},
	{Key: contracts.PermissionBillingManage},
}

// Module is the manifest. The implementation is constructed here, in one line,
// and handed to the two places that use it.
func Module(deps Deps) module.Module {
	if deps.Payments == nil {
		// A wiring mistake fails where it is written rather than as a nil
		// dereference in the worker at two in the morning.
		panic("billing.Module: Deps.Payments is required; wire billing.Manual() when there is no payment processor")
	}
	svc := internal.NewService()
	// The one thing about a plan that generic CRUD cannot know: a plan somebody
	// is still being billed for is not one an administrator may remove.
	mounted := spec
	mounted.AfterDelete = internal.RefuseWhileSubscribed
	return module.Module{
		Name:        "billing",
		Permissions: permissions,
		Events:      contracts.Events,
		Nav: []module.NavEntry{
			{Label: "Billing", Path: "/admin/billing/plans", Permission: contracts.PermissionBillingRead},
		},
		Jobs: []jobs.Job{internal.Renew(deps.Tenants, svc, deps.Payments, deps.RenewEvery)},
		// Written out so the absence is a decision: what a plan entitles
		// somebody to is the consuming module's business, and this module has no
		// opinion about anybody else's events.
		Subscriptions: nil,
		Routes: func(api *httpx.API) {
			mounted.Mount(api)
			internal.RegisterRoutes(api, svc)
		},
	}
}
