package contracts

import "github.com/septagon-oss/platformkit/kit/module"

// The two permissions a task has. They are named after the resource and not
// after the module, because a permission outlives the module that first defined
// it, and there are two of them rather than five because reading and changing
// are the only distinction anybody has yet wanted to grant separately.
const (
	PermissionTaskRead   = "task:read"
	PermissionTaskUpdate = "task:update"
)

// Permissions is what the manifest declares. kit/app checks every route's
// declaration against this list at boot, so a route guarded by a permission
// that is not here fails startup instead of denying everyone forever.
var Permissions = []module.Permission{
	{Key: PermissionTaskRead, Description: "Read tasks and their SLA state"},
	{Key: PermissionTaskUpdate, Description: "Create, change, assign and resolve tasks"},
}
