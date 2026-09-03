package contracts

// The three permissions billing has. They are named after the resource and not
// after the module, because a permission outlives the module that first defined
// it.
//
// The third is the seam the review found missing, and it is worth spelling out
// what it separates. Reading the price list, enrolling in a plan and *writing
// the price list* were one permission, billing:manage, held by every tenant's
// own administrator — so a customer created a plan of its own, at a price of
// its own, and switched to it. The review's probe did exactly that from
// past_due and its debt vanished.
//
// So the catalogue is the operator's. billing:catalog is declared
// Operator: true in the manifest and its routes with httpx.OperatorPermission,
// which the kernel refuses at any tenant but the operator's own before it asks
// the roles table anything: no wildcard satisfies it. billing:manage keeps
// what a customer legitimately does to its own subscription — subscribe, and
// cancel — and billing:read stays what it was, because every tenant has to be
// able to read the price list it is choosing from.
const (
	PermissionBillingRead    = "billing:read"
	PermissionBillingManage  = "billing:manage"
	PermissionBillingCatalog = "billing:catalog"
)
