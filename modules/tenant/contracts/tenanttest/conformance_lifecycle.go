package tenanttest

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// lifecycleCases are the five verbs the control plane was missing, written as the
// same executable specification the rest of this file is: they run against the fake
// here and against the real service over a real Postgres, so the two floors, the
// released slug and the pair of audit rows are one answer rather than two
// implementations drifting.
//
// Every case starts from a fixture that has an installation tenant
// (Fixture.Operator), because a lifecycle command that cannot write the operator's
// audit row writes nothing at all — see the last case.
func lifecycleCases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"reactivate resumes a suspended tenant":      reactivateTenant,
		"reactivating an active tenant says nothing": repeatReactivation,
		"rename changes the name and nothing else":   renameTenant,
		// The name is the one field of a tenant a body may hold, so these are the
		// refusals a form reads back — and the reason a name with somewhere to hide
		// is refused rather than trimmed is that the body said one thing and the row
		// would say another.
		"a rename that is not a name is refused and changes nothing": refuseInvalidName,
		"renaming a tenant to the name it has says nothing":          repeatRename,
		"remove-host stops serving one name":                         removeHost,
		// The two floors: refuse the write that takes the last one away, never the
		// write that finds none. Each refusal names the verb that would lift it,
		// because an immutable refusal that does not say what would change the world
		// is a dead end rather than an answer.
		"remove-host refuses a tenant's last host":                   preserveLastHost,
		"remove-host refuses the primary host":                       preservePrimaryHost,
		"removing a host the tenant does not answer at says nothing": removeAbsentHost,
		"delete needs the slug repeated":                             requireDeletionSlug,
		// The slug is released by the partial unique index of migrations/000006 — the
		// one reason deleted_at is a column rather than a third status, and the
		// reason a delete is reversible in the only sense the control plane cares
		// about: the customer's rows are where they were. The hosts are released by
		// the verb itself, and the case below is why: `tenant_hosts.host` is a global
		// key and a retired tenant cannot be allowed to reserve a name it does not
		// serve.
		"delete releases the slug and keeps the rows": deleteAndRetainRows,
		// The other half of the release, and the half a routing table cannot leave
		// out. `tenant_hosts.host` is the platform's key for "which customer is this
		// request for?", so a retired tenant that kept its rows would hold every
		// hostname it ever had forever: `RemoveHost` reaches a tenant through the
		// same `deleted_at` filter every read uses, and `AddHost` for the name would
		// be a conflict against a row no reader can find. What the delete keeps is
		// what only the customer read — its rows and its languages — and the pairing
		// of a retired customer with its names lives in `tenant.deleted` from here on.
		"delete releases the tenant's hosts as well as its slug":                  releaseDeletedHosts,
		"a deleted tenant is not reactivated, renamed, suspended or given a host": refuseDeletedTenant,
		"suspending, renaming and reactivating twice each say nothing":            repeatLifecycle,
		// The floor, and the case to read first: the installation's own tenant is the
		// door every one of these verbs is walked through, and a verb that closed it
		// could not be undone through it.
		"the installation's own tenant cannot be suspended or deleted": protectOperatorTenant,
		// The brief's requirement 2 as one case: the verb's own row in the customer's
		// scope and the installation's mirror beside it, both naming the one request
		// that caused them. This is the case that fails if the second row is ever
		// written with the wrong scope, or with no trace id.
		"one verb writes both audit rows with one trace id": pairAuditTrace,
	}
}

// acmeOperator is the installation's own tenant, which every fixture creates and no
// case is about. The name is used by the fixture harnesses, not by a case.
var acmeOperator = contracts.NewTenant{
	Slug: "installation", Name: "This installation", Host: "ops.example.com", Operator: true,
}
