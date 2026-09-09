package contracts

// Account review, profile reading and full management can be granted separately.
// Registration approval never grants permission to replace passwords or roles.
//
// user:manage is the permission an administrator holds. It is not the same as
// tenant:manage, which reaches every tenant: a user administrator changes their
// own tenant's people and nobody else's, because every query they cause runs
// inside their own tenant's transaction.
const (
	PermissionUserRead            = "user:read"
	PermissionUserManage          = "user:manage"
	PermissionRegistrationApprove = "user:approve"
)
