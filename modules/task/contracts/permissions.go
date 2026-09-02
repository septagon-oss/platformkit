package contracts

// The two permissions a task has. They are named after the resource and not
// after the module, because a permission outlives the module that first defined
// it, and there are two of them rather than five because reading and changing
// are the only distinction anybody has yet wanted to grant separately.
//
// The list the manifest declares is in ../module.go, with the manifest. Two
// strings are what a consumer needs to guard a route of its own; the
// module.Permission values around them are the kernel's shape, and keeping them
// out of here keeps kit/module out of this package's build graph — a contracts
// package is what a consumer compiles against, so everything it imports is
// something every consumer imports.
const (
	PermissionTaskRead   = "task:read"
	PermissionTaskUpdate = "task:update"
)
