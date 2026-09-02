package contracts

// The two permissions a user has. They are named after the resource and not
// after the module, and there are two rather than five because reading a
// colleague's profile and changing it are the only distinction anybody has yet
// wanted to grant separately.
//
// user:manage is the permission an administrator holds. It is not the same as
// tenant:manage, which reaches every tenant: a user administrator changes their
// own tenant's people and nobody else's, because every query they cause runs
// inside their own tenant's transaction.
const (
	PermissionUserRead   = "user:read"
	PermissionUserManage = "user:manage"
)
