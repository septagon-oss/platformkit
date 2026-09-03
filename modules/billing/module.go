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

// spec is the plan catalogue's presence in the application: five routes, three
// permissions, three events and the schema a generated screen reads.
//
// The catalogue is the operator's, and that is the one thing about this module
// worth reading before the rest.
//
// Plans used to be an ordinary tenant-scoped table, written under the same
// billing:manage that enrolls, which every tenant's own administrator holds by
// way of the wildcard their admin role carries. So a customer created a plan,
// at a price it chose, and subscribed to it: a review did exactly that from
// past_due and watched the debt disappear. A price list is not a thing a
// customer owns.
//
// The fix is one shared catalogue rather than a copy per tenant, and the choice
// between those two is worth writing down. Per-tenant copies seeded by the
// operator would keep the table tenant-scoped and cost a fan-out on every plan
// change, a reconciliation when one fails, and an answer to "what happens to
// the copy a tenant edited". One table, read by all and written by the
// operator, has none of those: migrations/000016 gives it USING (true) so every
// tenant sees the catalogue, and WITH CHECK on the tenant match so a row can
// only be written by a transaction scoped to the tenant it names. The routes
// carry the other half — OperatorWrite declares create, update and delete with
// httpx.OperatorPermission, which the kernel refuses at any tenant but the
// operator's own before it asks the roles table anything, and no wildcard
// satisfies it. See docs/adr/0008.
//
// It names no Immutable, and the absence is a decision: a plan is data the
// operator maintains, and changing a field changes what the *next* period
// costs, because the price a subscription is billed at is stamped on the
// subscription. The fields a command owns are all on the subscription, which is
// not a Spec. The one hook it does have, on delete, declares no HookEvents
// because all it ever does is refuse.
var spec = rest.Spec[*contracts.Plan]{
	Module:        "billing",
	Entity:        "plan",
	Path:          "/api/v1/billing/plans",
	Read:          contracts.PermissionBillingRead,
	Write:         contracts.PermissionBillingCatalog,
	OperatorWrite: true,
	SoftDelete:    true,
}

// permissions is what the manifest declares. kit/app checks every route's
// declaration against it at boot, so a route guarded by a permission that is
// not here fails startup instead of denying everyone forever — and, for the
// third one, so that a route declaring httpx.Permission where the manifest says
// Operator would fail startup rather than quietly letting a customer's wildcard
// into the price list.
var permissions = []module.Permission{
	{Key: contracts.PermissionBillingRead},
	{Key: contracts.PermissionBillingManage},
	{Key: contracts.PermissionBillingCatalog, Operator: true},
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
			// The one thing about a plan that generic CRUD cannot know: a plan
			// somebody is still being billed for is not one the operator may
			// remove. The hook takes the system capability because the question
			// crosses every tenant — the catalogue is shared, and the operator's
			// own transaction can see only the operator's subscriptions.
			mounted := spec
			mounted.AfterDelete = internal.RefuseWhileSubscribed(api.SystemToken())
			mounted.Mount(api)
			internal.RegisterRoutes(api, svc)
		},
	}
}
