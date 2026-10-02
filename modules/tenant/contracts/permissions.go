package contracts

// PermissionTenantManage guards every route in this module. There is no
// separate read permission: the control plane is one thing to be trusted with,
// and a role that may look at the list of every customer is a role that may
// change it.
const PermissionTenantManage = "tenant:manage"

// PermissionLocaleManage is a tenant's own: it lets that tenant's administrators
// say which languages their people are answered in. It is a separate permission
// rather than a corner of tenant:manage because tenant:manage is the operator's —
// declared Operator, so a customer's administrator cannot hold it at all — and the
// languages of one customer's pages are not a fact about every customer. The
// module declares it; which role is handed it is the composition's and its
// product's decision, as it is for every permission.
const PermissionLocaleManage = "tenant:locale_manage"
