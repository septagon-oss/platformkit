// The permission keys this module defines. A key belongs here rather than beside
// the route that will use it because kit/app refuses to start a route whose
// permission no manifest defines, so the module that owns the rows has to name
// the key before any page can guard itself with it.
package contracts

// PermissionSenderManage guards the tenant's own sending address: who may put
// its name, its address and its DKIM selector, ask whether its domain vouches
// for it, and remove one that is not believed. It is not a permission every
// signed-in person holds, which is why the notification routes themselves do not
// use it — a person's own notifications and their own channel switches are scoped
// by the principal — while the tenant's mail identity is scoped by this key.
const PermissionSenderManage = "sender:manage"
