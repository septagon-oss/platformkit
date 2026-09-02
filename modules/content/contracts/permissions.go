package contracts

// The two permissions content has. They are named after the resource and not
// after the module, because a permission outlives the module that first defined
// it, and publishing is not a permission of its own: whoever may change what a
// page says may decide that it says it in public.
//
// The list the manifest declares is in ../module.go, which keeps kit/module out
// of this package's build graph.
const (
	PermissionContentRead   = "content:read"
	PermissionContentManage = "content:manage"
)
