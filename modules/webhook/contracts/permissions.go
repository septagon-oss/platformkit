package contracts

// The two doors this module has. They are named after the resource they guard
// and not after the module, and they are two because reading a delivery log is
// not the same act as registering an address the platform will POST to: the
// first is what an on-call engineer does at three in the morning, the second is
// what changes where a tenant's events go.
//
// Which person of a tenant holds either is the composition's decision. This
// module declares no role: a subscriber address is a tenant's own row, and
// saying which of its people may register one is a product's fact, not the
// capability's.
const (
	// PermissionWebhookRead guards the endpoint list and read and the delivery
	// log. It never reaches the secret: no readable field carries it.
	PermissionWebhookRead = "webhook:read"

	// PermissionWebhookUpdate guards every write: the create, the name change,
	// the target change, rotation, enablement, disablement and a replay — a
	// replay sends, so it is a write and never a read.
	PermissionWebhookUpdate = "webhook:update"
)
