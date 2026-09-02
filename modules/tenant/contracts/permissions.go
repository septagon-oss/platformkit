package contracts

// PermissionTenantManage guards every route in this module. There is no
// separate read permission: the control plane is one thing to be trusted with,
// and a role that may look at the list of every customer is a role that may
// change it.
const PermissionTenantManage = "tenant:manage"
